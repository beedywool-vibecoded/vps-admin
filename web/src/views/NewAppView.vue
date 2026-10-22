<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import Icon from '@/components/Icon.vue'
import M3Shape from '@/components/M3Shape.vue'
import M3Switch from '@/components/M3Switch.vue'
import EnvEditor from '@/components/EnvEditor.vue'
import M3Select from '@/components/M3Select.vue'
import BuildCommands, { type Commands } from '@/components/BuildCommands.vue'
import AccessPicker from '@/components/AccessPicker.vue'
import { api, type BuildType, type AccessMode, type Detection, type EnvVar, type GitHubStatus, type Repo } from '@/lib/api'
import { BUILD_LABEL, slugify, suggestDomain } from '@/lib/deploys'
import { ago } from '@/lib/format'
import { toastError } from '@/lib/ui'

const router = useRouter()
const gh = ref<GitHubStatus | null>(null)
const repos = ref<Repo[]>([])
const loadingRepos = ref(true)
const query = ref('')
const repo = ref<Repo | null>(null)
const detection = ref<Detection | null>(null)
const detecting = ref(false)
const saving = ref(false)

const name = ref('')
const branch = ref('')
const branches = ref<string[]>([])
const branchOptions = computed(() => [...new Set([branch.value, ...branches.value].filter(Boolean))].map((b) => ({ value: b, label: b })))
const rootDir = ref('')
const build = ref<BuildType>('auto')
const port = ref(0)
const cmds = ref<Commands>({ installCmd: '', buildCmd: '', startCmd: '', dockerfile: '', composeFile: '' })
const domain = ref('')
const access = ref<AccessMode>('public')
const allow = ref('')
const autoDeploy = ref(true)
const keep = ref(5)
const memoryMb = ref(0)
const env = ref<EnvVar[]>([])

const shown = computed(() => {
  const q = query.value.trim().toLowerCase()
  return repos.value.filter((r) => !q || r.fullName.toLowerCase().includes(q)).sort((a, b) => b.pushedAt.localeCompare(a.pushedAt))
})
const effectiveBuild = computed(() => (build.value === 'auto' ? detection.value?.build || '' : build.value))
const needsPort = computed(() => effectiveBuild.value === 'dockerfile' && !port.value)
const step = computed(() => (repo.value ? 2 : 1))
const buildOptions = (Object.keys(BUILD_LABEL) as BuildType[]).map((k) => ({ value: k, label: BUILD_LABEL[k]! }))

function pick(r: Repo) {
  repo.value = r
  name.value = slugify(r.fullName.split('/')[1] ?? r.fullName)
  branch.value = r.defaultBranch
  branches.value = []
  api
    .branches(r.installationId, r.fullName)
    .then((res) => repo.value === r && (branches.value = res.branches))
    .catch(() => {})
  rootDir.value = ''
  build.value = 'auto'
  port.value = 0
  domain.value = gh.value?.domains ? suggestDomain(gh.value.publicUrl, name.value) : ''
  detect()
}

let detectTimer: ReturnType<typeof setTimeout> | undefined
async function detect() {
  if (!repo.value) return
  detecting.value = true
  try {
    const res = await api.detect(repo.value, branch.value, rootDir.value)
    detection.value = res.detection
    if (res.detection.port && (!port.value || build.value === 'auto')) port.value = res.detection.port
  } catch (e) {
    detection.value = { build: '', port: 0, reason: e instanceof Error ? e.message : String(e) }
  } finally {
    detecting.value = false
  }
}
watch([branch, rootDir], () => {
  clearTimeout(detectTimer)
  detectTimer = setTimeout(detect, 500)
})
watch(name, (n, old) => {
  if (gh.value?.domains && (!domain.value || domain.value === suggestDomain(gh.value.publicUrl, old))) {
    domain.value = suggestDomain(gh.value.publicUrl, n)
  }
})

async function submit() {
  if (!repo.value) return
  saving.value = true
  try {
    const res = await api.createApp({
      id: name.value,
      repo: repo.value.fullName,
      branch: branch.value,
      rootDir: rootDir.value,
      build: build.value,
      port: port.value || 0,
      domain: access.value === 'public' || access.value === 'vpn' ? domain.value : '',
      access: access.value,
      allow: allow.value,
      autoDeploy: autoDeploy.value,
      keep: keep.value,
      memoryMb: memoryMb.value || 0,
      ...cmds.value,
      env: env.value,
      deploy: true,
    })
    router.push({ path: `/apps/${res.app.id}`, query: res.deployment ? { dep: String(res.deployment.id) } : {} })
  } catch (e) {
    toastError(e)
  } finally {
    saving.value = false
  }
}

// Both cards scroll on their own, so each stops where the window does instead of running past
// the fold and pushing the page: it gets the height left below its own top edge.
const sourceCard = ref<HTMLElement | null>(null)
const configCard = ref<HTMLElement | null>(null)
const sourceMax = ref(0)
const configMax = ref(0)
function capFor(el: HTMLElement | null, floor: number) {
  if (!el) return 0
  // The page keeps its own bottom padding, which is what the window shows below the card.
  const shell = el.closest('.main-col')
  const below = shell ? parseFloat(getComputedStyle(shell).paddingBottom) || 0 : 24
  const avail = Math.round(window.innerHeight - el.getBoundingClientRect().top - below)
  // A scroll box too short to show a field is worse than the page scrolling: stacked under
  // the other card, or on a window with only a sliver left, the card keeps its own height.
  return avail < floor ? 0 : avail
}
function fitCards() {
  sourceMax.value = capFor(sourceCard.value, 240)
  configMax.value = capFor(configCard.value, 300)
}
// Picking a repository adds the settings card, which is another card to fit.
watch(repo, async () => {
  await nextTick()
  fitCards()
})

onMounted(async () => {
  window.addEventListener('resize', fitCards)
  try {
    gh.value = await api.github()
    if (!gh.value.connected) return router.replace('/apps')
    repos.value = (await api.repos()).repos
  } catch (e) {
    toastError(e)
  } finally {
    loadingRepos.value = false
    await nextTick()
    fitCards()
  }
})
onBeforeUnmount(() => window.removeEventListener('resize', fitCards))
</script>

<template>
  <div class="page">
    <div class="page-title">
      <RouterLink to="/apps" class="icon-btn st" aria-label="Back to apps"><Icon name="back" /></RouterLink>
      <h1>New app</h1>
    </div>

    <ol class="steps" aria-label="Steps">
      <li :class="{ current: step === 1, done: step > 1 }"><span>{{ step > 1 ? '✓' : 1 }}</span>Source</li>
      <li :class="{ current: step === 2 }"><span>2</span>Build and settings</li>
      <li><span>3</span>Deploy</li>
    </ol>

    <div class="cols">
      <section ref="sourceCard" class="card source" :style="{ maxHeight: sourceMax ? `${sourceMax}px` : undefined }">
        <h2>Pick a repository</h2>
        <label class="field search">
          <Icon name="search" :size="20" />
          <input v-model="query" placeholder="Search repositories" aria-label="Search repositories" />
        </label>
        <p v-if="loadingRepos" class="muted">Loading repositories…</p>
        <p v-else-if="!repos.length" class="muted small">
          The GitHub App cannot read any repository yet.
          <a :href="gh?.app?.installUrl" target="_blank" rel="noopener">Choose repositories on GitHub</a>, then reload.
        </p>
        <div class="repos" role="listbox" aria-label="Repositories">
          <button
            v-for="r in shown"
            :key="r.fullName"
            type="button"
            role="option"
            class="repo st"
            :class="{ sel: repo?.fullName === r.fullName }"
            :aria-selected="repo?.fullName === r.fullName"
            @click="pick(r)"
          >
            <span class="grow">
              <span class="rname">{{ r.fullName.split('/')[1] }}</span>
              <span class="muted small">
                {{ r.fullName.split('/')[0] }}<template v-if="r.language"> · {{ r.language }}</template> · pushed {{ ago(r.pushedAt) }}
              </span>
            </span>
            <Icon v-if="r.private" name="lock" :size="16" />
          </button>
        </div>
      </section>

      <section v-if="repo" ref="configCard" class="card config" :style="{ maxHeight: configMax ? `${configMax}px` : undefined }">
        <div class="cfg-head">
          <M3Shape kind="cookie" :size="44" fill="var(--pc)" />
          <div class="grow">
            <h2>{{ repo.fullName.split('/')[1] }}</h2>
            <div class="muted small">{{ repo.fullName }}{{ repo.private ? ' · private' : '' }}</div>
          </div>
        </div>

        <div class="grid">
          <label class="tf">
            <span>Name</span>
            <input :value="name" spellcheck="false" @input="name = slugify(($event.target as HTMLInputElement).value)" />
            <small>Also the container name</small>
          </label>
          <M3Select v-model="branch" label="Branch" :options="branchOptions" />
          <label class="tf">
            <span>Root folder</span>
            <input v-model.trim="rootDir" placeholder="/" spellcheck="false" />
            <small>For a monorepo, the folder of this app</small>
          </label>
        </div>

        <div>
          <h3 class="sub">Access</h3>
          <AccessPicker v-model:access="access" v-model:domain="domain" v-model:allow="allow" />
        </div>

        <div class="tile">
          <M3Shape kind="clover" :size="36" fill="var(--secc)" />
          <div class="grow">
            <div class="tile-title">{{ detecting ? 'Looking at the repository…' : detection?.build ? `Detected: ${BUILD_LABEL[detection.build]}` : 'Build' }}</div>
            <div class="muted small">{{ detection?.reason }}</div>
          </div>
          <M3Select v-model="build" class="build-sel" label="Build" :options="buildOptions" />
        </div>

        <div class="grid">
          <label class="tf">
            <span>{{ effectiveBuild === 'compose' ? 'Published port to route the domain to' : 'Port the app listens on' }}</span>
            <input v-model.number="port" inputmode="numeric" :class="{ warn: needsPort }" />
            <small>{{ effectiveBuild === 'compose' ? 'A port your compose file publishes on 127.0.0.1' : needsPort ? 'Required: the Dockerfile has no EXPOSE line' : 'Inside the container' }}</small>
          </label>
          <label class="tf">
            <span>Memory limit (MiB)</span>
            <input v-model.number="memoryMb" inputmode="numeric" placeholder="0" />
            <small>0 for no limit</small>
          </label>
        </div>

        <BuildCommands v-model="cmds" :build="effectiveBuild" />

        <div>
          <h3 class="sub">Environment variables</h3>
          <EnvEditor v-model="env" />
        </div>

        <div class="setting">
          <div class="grow">
            <div>Deploy on every push</div>
            <div class="muted small">GitHub notifies the server; the new version replaces the old one without downtime</div>
          </div>
          <M3Switch v-model="autoDeploy" label="Deploy on every push" />
        </div>
        <div class="setting">
          <div class="grow">
            <div>Versions kept for rollback</div>
            <div class="muted small">Older images are removed after each deploy</div>
          </div>
          <input v-model.number="keep" class="num" inputmode="numeric" aria-label="Versions kept" />
        </div>

        <div class="actions">
          <button class="btn btn-filled btn-lg st" type="button" :disabled="saving || !name || !branch || needsPort || (!effectiveBuild && !detecting)" @click="submit">
            {{ saving ? 'Creating…' : 'Deploy' }}
          </button>
        </div>
      </section>
    </div>
  </div>
</template>

<style scoped>
.steps {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
.steps li {
  display: flex;
  align-items: center;
  gap: 10px;
  height: 44px;
  padding: 0 18px 0 8px;
  border-radius: 22px;
  background: var(--sc);
  color: var(--on-sv);
  font-weight: 500;
}
.steps span {
  width: 28px;
  height: 28px;
  border-radius: 14px;
  display: grid;
  place-items: center;
  background: var(--outline-v);
  font-size: 13px;
  font-weight: 700;
}
.steps li.current {
  background: var(--pc);
  color: var(--on-pc);
}
.steps li.current span,
.steps li.done span {
  background: var(--primary);
  color: var(--on-primary);
}
.cols {
  display: flex;
  flex-wrap: wrap;
  gap: 16px;
  align-items: flex-start;
}
.source {
  flex: 1 1 min(380px, 100%);
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 14px;
}
.source h2,
.config h2 {
  font-size: 20px;
}
.repos {
  display: flex;
  flex-direction: column;
  gap: 6px;
  /* The list is what shrinks when the window is short: it already scrolls on its own. */
  flex: 1 1 auto;
  min-height: 0;
  overflow: auto;
}
.repo {
  /* the state layer clips (overflow: hidden), which would let the list squash rows */
  flex-shrink: 0;
  display: flex;
  align-items: center;
  gap: 12px;
  min-height: 64px;
  padding: 12px 16px;
  border-radius: 16px;
  border: 2px solid transparent;
  background: transparent;
  color: var(--on-surface);
  text-align: left;
}
.repo .grow {
  display: flex;
  flex-direction: column;
}
.repo.sel {
  background: var(--pc);
  border-color: var(--primary);
  color: var(--on-pc);
}
.rname {
  font-size: 15px;
  font-weight: 500;
}
.config {
  flex: 1.4 1 min(520px, 100%);
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 18px;
  /* The settings are taller than the window long before the page is: they scroll here.
     The dropdowns of the fields are drawn on the body, so nothing gets clipped. */
  overflow: auto;
}
/* Fields keep their own height while the card scrolls, instead of being squashed to fit. */
.config > * {
  flex-shrink: 0;
}
.cfg-head {
  display: flex;
  align-items: center;
  gap: 12px;
}
.grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
  gap: 12px;
}
.tf {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 10px 16px 8px;
  border-radius: 12px 12px 4px 4px;
  background: var(--schh);
  border-bottom: 1px solid var(--on-sv);
}
.tf:focus-within {
  border-bottom-color: var(--primary);
  box-shadow: inset 0 -1px 0 var(--primary);
}
.tf span {
  font-size: 12px;
  font-weight: 500;
  color: var(--primary);
}
.tf input {
  border: none;
  background: none;
  outline: none;
  font-size: 16px;
  color: var(--on-surface);
  padding: 2px 0;
}
.tf input.warn {
  color: var(--warn);
}
.tf small {
  font-size: 11px;
  color: var(--on-sv);
}
.tile {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 14px;
  padding: 14px 16px;
  border-radius: 18px;
  background: var(--sch);
}
.tile-title {
  font-weight: 500;
}
.build-sel {
  min-width: 230px;
}
.num {
  height: 40px;
  padding: 0 12px;
  border-radius: 20px;
  border: 1px solid var(--outline);
  background: var(--sc);
  color: var(--on-surface);
}
.num {
  width: 80px;
  text-align: center;
}
.sub {
  font-size: 16px;
  margin-bottom: 8px;
}
.setting {
  display: flex;
  align-items: center;
  gap: 16px;
}
.actions {
  display: flex;
  justify-content: flex-end;
}
</style>
