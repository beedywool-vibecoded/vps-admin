<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import Icon from '@/components/Icon.vue'
import M3Shape from '@/components/M3Shape.vue'
import ShapeLoader from '@/components/ShapeLoader.vue'
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
const step = ref<1 | 2 | 3>(1)
const buildOptions = (Object.keys(BUILD_LABEL) as BuildType[]).map((k) => ({ value: k, label: BUILD_LABEL[k]! }))
const invalid = computed(() => !name.value || !branch.value || needsPort.value || (!effectiveBuild.value && !detecting.value))
const accessText = computed(() => {
  if (access.value === 'public') return domain.value ? `Public domain · ${domain.value}` : 'Public domain'
  if (access.value === 'vpn') return allow.value ? `VPN only · ${allow.value}` : 'VPN only'
  if (access.value === 'tailscale') return 'Tailscale only'
  return 'No domain'
})
const envCount = computed(() => env.value.filter((v) => v.key.trim()).length)

function go(n: 1 | 2 | 3) {
  if (n > 1 && !repo.value) return
  step.value = n
}

function pick(r: Repo) {
  // Coming back to the step keeps the settings of the repository already chosen.
  if (repo.value?.fullName === r.fullName) {
    step.value = 2
    return
  }
  repo.value = r
  step.value = 2
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

onMounted(async () => {
  try {
    gh.value = await api.github()
    if (!gh.value.connected) return router.replace('/apps')
    repos.value = (await api.repos()).repos
  } catch (e) {
    toastError(e)
  } finally {
    loadingRepos.value = false
  }
})
</script>

<template>
  <div class="page">
    <div class="page-title">
      <RouterLink to="/apps" class="icon-btn st" aria-label="Back to apps"><Icon name="back" /></RouterLink>
      <h1>New app</h1>
    </div>

    <ol class="steps" aria-label="New app steps">
      <li :class="{ current: step === 1, done: step > 1 }">
        <button type="button" class="step st" :aria-current="step === 1 ? 'step' : undefined" @click="go(1)">
          <span><Icon v-if="step > 1" name="check" :size="18" /><template v-else>1</template></span>Source
        </button>
      </li>
      <li :class="{ current: step === 2, done: step > 2, locked: !repo }">
        <button type="button" class="step st" :aria-current="step === 2 ? 'step' : undefined" :disabled="!repo" @click="go(2)">
          <span><Icon v-if="step > 2" name="check" :size="18" /><template v-else>2</template></span>Build and settings
        </button>
      </li>
      <li :class="{ current: step === 3, locked: !repo }">
        <button type="button" class="step st" :aria-current="step === 3 ? 'step' : undefined" :disabled="!repo" @click="go(3)">
          <span>3</span>Deploy
        </button>
      </li>
    </ol>

    <section v-if="step === 1" class="card source">
        <h2>Pick a repository</h2>
        <label class="field search">
          <Icon name="search" :size="20" />
          <input v-model="query" placeholder="Search repositories" aria-label="Search repositories" />
        </label>
        <div v-if="loadingRepos" class="loading-block"><ShapeLoader label="Loading repositories" /></div>
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
        <div v-if="repo" class="actions">
          <button class="btn btn-filled btn-lg st" type="button" @click="step = 2">Continue</button>
        </div>
    </section>

      <section v-else-if="step === 2 && repo" class="card config">
        <div class="cfg-head">
          <M3Shape kind="cookie" :size="44" fill="var(--pc)" />
          <div class="grow">
            <h2>{{ repo.fullName.split('/')[1] }}</h2>
            <div class="muted small">{{ repo.fullName }}{{ repo.private ? ' · private' : '' }}</div>
          </div>
          <button class="btn btn-text st" type="button" @click="step = 1">Change</button>
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
          <button class="btn btn-text st" type="button" @click="step = 1">Back</button>
          <button class="btn btn-filled btn-lg st" type="button" :disabled="invalid" @click="step = 3">Continue</button>
        </div>
      </section>

      <section v-else-if="step === 3 && repo" class="card config review-card">
        <div class="cfg-head">
          <M3Shape kind="cookie" :size="44" fill="var(--pc)" />
          <div class="grow">
            <h2>Ready to deploy</h2>
            <div class="muted small">{{ repo.fullName }}{{ repo.private ? ' · private' : '' }}</div>
          </div>
        </div>

        <dl class="review">
          <div><dt>Name</dt><dd>{{ name }}</dd></div>
          <div><dt>Branch</dt><dd>{{ branch }}</dd></div>
          <div><dt>Root folder</dt><dd>{{ rootDir || '/' }}</dd></div>
          <div><dt>Access</dt><dd>{{ accessText }}</dd></div>
          <div><dt>Build</dt><dd>{{ BUILD_LABEL[effectiveBuild] ?? (detecting ? 'Detecting…' : 'Not detected') }}</dd></div>
          <div><dt>Port</dt><dd>{{ port || '—' }}</dd></div>
          <div><dt>Memory limit</dt><dd>{{ memoryMb ? `${memoryMb} MiB` : 'No limit' }}</dd></div>
          <div><dt>Environment</dt><dd>{{ envCount ? `${envCount} variable${envCount > 1 ? 's' : ''}` : 'None' }}</dd></div>
          <div><dt>Auto-deploy</dt><dd>{{ autoDeploy ? 'On every push' : 'Off' }}</dd></div>
          <div><dt>Versions kept</dt><dd>{{ keep }}</dd></div>
        </dl>

        <p v-if="detection?.reason" class="muted small">{{ detection.reason }}</p>
        <p class="muted small">The app is created and its first deployment starts immediately.</p>

        <div class="actions">
          <button class="btn btn-text st" type="button" @click="step = 2">Back</button>
          <button class="btn btn-filled btn-lg st" type="button" :disabled="saving || invalid" @click="submit">
            <ShapeLoader v-if="saving" small label="Creating app" />
            {{ saving ? 'Creating…' : 'Deploy' }}
          </button>
        </div>
      </section>
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
  border-radius: 22px;
  background: var(--sc);
  color: var(--on-sv);
  font-weight: 500;
}
.steps li.locked {
  opacity: 0.5;
}
.step {
  display: flex;
  align-items: center;
  gap: 10px;
  height: 44px;
  padding: 0 18px 0 8px;
  border: none;
  border-radius: 22px;
  background: none;
  color: inherit;
  font: inherit;
  font-weight: 500;
}
.step:disabled {
  cursor: default;
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
.source {
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
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 18px;
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
.review {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(240px, 1fr));
  gap: 12px;
  margin: 0;
}
.review > div {
  padding: 12px 16px;
  border-radius: 16px;
  background: var(--sch);
}
.review dt {
  margin-bottom: 4px;
  font-size: 12px;
  color: var(--on-sv);
}
.review dd {
  margin: 0;
  font-size: 15px;
  overflow-wrap: anywhere;
}
.actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}
</style>
