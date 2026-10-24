<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import Icon from '@/components/Icon.vue'
import M3Shape from '@/components/M3Shape.vue'
import ShapeLoader from '@/components/ShapeLoader.vue'
import StatusDot from '@/components/StatusDot.vue'
import { api, type AppView, type GitHubStatus } from '@/lib/api'
import { BUILD_LABEL, STATUS_LABEL, appTone } from '@/lib/deploys'
import { ago } from '@/lib/format'
import { confirm, toast, toastError } from '@/lib/ui'
import type { ShapeKind } from '@/lib/shapes'

const route = useRoute()
const apps = ref<AppView[]>([])
const gh = ref<GitHubStatus | null>(null)
const loading = ref(true)
const connecting = ref(false)
const SHAPES: ShapeKind[] = ['cookie', 'clover', 'sunny', 'flower']

async function load() {
  try {
    const [a, g] = await Promise.all([api.apps(), api.github()])
    apps.value = a.apps
    gh.value = g
  } catch (e) {
    toastError(e)
  } finally {
    loading.value = false
  }
}

// GitHub's manifest flow is a top-level form POST; the agent receives the result.
async function connect() {
  connecting.value = true
  try {
    const { url, manifest } = await api.githubManifest()
    const form = document.createElement('form')
    form.method = 'post'
    form.action = url
    const input = document.createElement('input')
    input.type = 'hidden'
    input.name = 'manifest'
    input.value = manifest
    form.append(input)
    document.body.append(form)
    form.submit()
  } catch (e) {
    toastError(e)
    connecting.value = false
  }
}

async function disconnect() {
  const ok = await confirm({
    title: 'Disconnect GitHub?',
    body: 'Apps keep running, but cannot be redeployed until GitHub is connected again. Delete the App on GitHub too if you no longer need it.',
    action: 'Disconnect',
    danger: true,
  })
  if (!ok) return
  try {
    await api.githubDisconnect()
    await load()
  } catch (e) {
    toastError(e)
  }
}

let timer: ReturnType<typeof setInterval> | undefined
onMounted(() => {
  const g = route.query.github
  if (g === 'installed') toast('GitHub is connected. Pick a repository to deploy.')
  if (g === 'failed' || g === 'expired') toast('GitHub did not finish connecting, try again.', true)
  load()
  timer = setInterval(load, 10000)
})
onBeforeUnmount(() => clearInterval(timer))
</script>

<template>
  <div class="page">
    <div class="page-title">
      <h1>Apps</h1>
      <RouterLink to="/apps/new" class="btn btn-filled btn-lg st"><Icon name="plus" :size="20" /> New app</RouterLink>
    </div>

    <section v-if="gh && !gh.connected" class="card connect">
      <Icon name="github" />
      <div class="grow">
        <h2>Deploy from GitHub</h2>
        <p class="muted">
          Creates a private GitHub App for this server in one click: it can only read the repositories you
          choose, and tells the server when you push so it redeploys by itself. GitHub then lets
          you pick your account or any of your organizations.
        </p>
        <p class="muted small">
          You can also deploy any public repository from its URL without connecting GitHub, using
          <RouterLink to="/apps/new">New app</RouterLink>.
        </p>
      </div>
      <button class="btn btn-filled btn-lg st" type="button" :disabled="connecting" @click="connect">
        <Icon name="github" :size="20" /> Connect GitHub
      </button>
    </section>

    <section v-else-if="gh?.connected" class="card gh">
      <Icon name="github" :size="28" />
      <div class="grow">
        <div class="name">{{ gh.app?.owner }}</div>
        <div class="muted small">
          {{ gh.app?.name }} ·
          {{ gh.installations?.length ? `installed on ${gh.installations.map((i) => i.account).join(', ')}` : 'not installed on any account yet' }}
        </div>
      </div>
      <a :href="gh.app?.installUrl" class="btn btn-text st" target="_blank" rel="noopener">Choose repositories</a>
      <button class="btn btn-text st" type="button" @click="disconnect">Disconnect</button>
    </section>

    <p v-if="gh && !gh.domains" class="muted small">
      No reverse proxy (Caddy or nginx) was set up by the installer, so apps get no public domain. They can still be served on Tailscale or stay on 127.0.0.1.
    </p>

    <div v-if="loading" class="loading-block"><ShapeLoader label="Loading apps" /></div>
    <section v-else-if="!apps.length && gh?.connected" class="card empty">
      <M3Shape kind="flower" :size="64" fill="var(--pc)" />
      <p class="muted">No app yet. Pick a repository and it is online in a few minutes.</p>
      <RouterLink to="/apps/new" class="btn btn-tonal st">New app</RouterLink>
    </section>

    <RouterLink v-for="(a, i) in apps" :key="a.id" :to="`/apps/${a.id}`" class="card app st">
      <M3Shape :kind="SHAPES[i % SHAPES.length]!" :size="48" :fill="i % 2 ? 'var(--secc)' : 'var(--pc)'" />
      <div class="grow">
        <div class="name">
          <StatusDot :tone="appTone(a.status, a.latest)" :label="a.status" />
          {{ a.id }}
          <span v-if="a.dir" class="chip folder-chip" v-tip="a.dir"><Icon name="folder" :size="14" /> Existing folder</span>
        </div>
        <div class="muted small ellipsis">
          {{ a.repo }} · {{ a.branch }} · {{ BUILD_LABEL[a.build] }}<template v-if="a.domain"> · {{ a.domain }}</template>
        </div>
      </div>
      <div class="deploy small">
        <template v-if="a.latest">
          <span class="chip" :class="a.latest.status">{{ STATUS_LABEL[a.latest.status] }}</span>
          <span class="muted">{{ a.latest.sha.slice(0, 7) }} · {{ ago(a.latest.createdAt) }}</span>
        </template>
        <span v-else class="muted">Never deployed</span>
      </div>
      <Icon name="chevron-right" />
    </RouterLink>
  </div>
</template>

<style scoped>
.folder-chip {
  gap: 4px;
  font-weight: 400;
}
.connect {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 20px;
}
.connect h2 {
  font-size: 22px;
  margin-bottom: 6px;
}
.connect p {
  margin: 0 0 8px;
  max-width: 640px;
}
.gh {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 14px;
  padding: 14px 18px;
}
.empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 12px;
  padding: 36px;
  text-align: center;
}
.app {
  display: flex;
  align-items: center;
  gap: 16px;
  color: inherit;
  text-decoration: none;
}
.name {
  display: flex;
  align-items: center;
  gap: 10px;
  font-size: 17px;
  font-weight: 500;
}
.deploy {
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  gap: 4px;
}
.chip.live {
  background: var(--ok);
  color: var(--on-ok);
}
.chip.failed {
  background: var(--bad);
  color: var(--on-bad);
}
.chip.building,
.chip.starting,
.chip.queued {
  background: var(--warn);
  color: var(--on-warn);
}
@media (max-width: 760px) {
  .deploy {
    display: none;
  }
}
</style>
