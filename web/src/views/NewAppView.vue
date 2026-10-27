<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import Icon from '@/components/Icon.vue'
import M3Shape from '@/components/M3Shape.vue'
import ShapeLoader from '@/components/ShapeLoader.vue'
import M3Switch from '@/components/M3Switch.vue'
import EnvEditor from '@/components/EnvEditor.vue'
import EnvTarget from '@/components/EnvTarget.vue'
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
const account = ref('all')
const accountOptions = computed(() => [
  { value: 'all', label: 'All accounts' },
  ...(gh.value?.installations ?? []).map((i) => ({
    value: String(i.id),
    label: i.account,
    hint: i.type === 'Organization' ? 'Organization' : 'Personal account',
  })),
])
const repo = ref<Repo | null>(null)
const mode = ref<'github' | 'public'>('github')
const publicUrl = ref('')
const publicError = ref('')
const resolving = ref(false)
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
const envFile = ref(false)
const envPath = ref('')

const shown = computed(() => {
  const q = query.value.trim().toLowerCase()
  return repos.value
    .filter((r) => (account.value === 'all' || String(r.installationId) === account.value) && (!q || r.fullName.toLowerCase().includes(q)))
    .sort((a, b) => b.pushedAt.localeCompare(a.pushedAt))
})
const effectiveBuild = computed(() => (build.value === 'auto' ? detection.value?.build || '' : build.value))
const needsPort = computed(() => effectiveBuild.value === 'dockerfile' && !port.value)
// A public repository has no push webhook, so automatic deploys cannot happen.
const isExternal = computed(() => repo.value?.installationId === 0)
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
// What the server detected, worded as a fact.
const detected = computed(() => {
  if (detecting.value) return 'Looking for a build file…'
  const b = detection.value?.build
  return b ? `${BUILD_LABEL[b]} detected` : 'No build file found'
})

// Each step card scrolls inside itself instead of pushing the page.
const cardEl = ref<HTMLElement | null>(null)
const cardMax = ref(0)
function fitCard() {
  const el = cardEl.value
  if (!el) return
  // The window shows the page's bottom padding below the card.
  const shell = el.closest('.main-col')
  const below = shell ? parseFloat(getComputedStyle(shell).paddingBottom) || 0 : 24
  const avail = Math.round(window.innerHeight - el.getBoundingClientRect().top - below)
  // Too short to show a field: let the page scroll instead.
  cardMax.value = avail < 240 ? 0 : avail
}
watch(step, async () => {
  await nextTick()
  fitCard()
})

function go(n: 1 | 2 | 3) {
  if (n > 1 && !repo.value) return
  step.value = n
}

function pick(r: Repo) {
  // Keep the settings when the same repository is chosen again.
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
  autoDeploy.value = r.installationId !== 0
  domain.value = gh.value?.domains ? suggestDomain(gh.value.publicUrl, name.value) : ''
  detect()
}

// Public URLs are read without the GitHub App. No webhook, so deploys are manual.
async function usePublic() {
  if (!publicUrl.value.trim()) return
  resolving.value = true
  publicError.value = ''
  try {
    const res = await api.publicRepo(publicUrl.value)
    publicUrl.value = ''
    pick(res.repo)
  } catch (e) {
    publicError.value = e instanceof Error ? e.message : String(e)
  } finally {
    resolving.value = false
  }
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
      envFile: envFile.value,
      envPath: envPath.value,
      deploy: true,
      external: isExternal.value,
    })
    router.push({ path: `/apps/${res.app.id}`, query: res.deployment ? { dep: String(res.deployment.id) } : {} })
  } catch (e) {
    toastError(e)
  } finally {
    saving.value = false
  }
}

onMounted(async () => {
  window.addEventListener('resize', fitCard)
  try {
    gh.value = await api.github()
    if (gh.value.connected) repos.value = (await api.repos()).repos
    else mode.value = 'public'
  } catch (e) {
    mode.value = 'public'
    toastError(e)
  } finally {
    loadingRepos.value = false
    await nextTick()
    fitCard()
  }
})
onBeforeUnmount(() => window.removeEventListener('resize', fitCard))
</script>

<template>
  <div class="page">
    <div class="page-title">
      <RouterLink to="/apps" class="icon-btn st" aria-label="Back to apps"><Icon name="back" /></RouterLink>
      <h1>New app</h1>
    </div>

    <ol class="steps" aria-label="New app steps">
      <li :class="{ current: step === 1, done: step > 1 }">
        <button type="button" class="step st" aria-label="Step 1: Source" :aria-current="step === 1 ? 'step' : undefined" @click="go(1)">
          <span><Icon v-if="step > 1" name="check" :size="18" /><template v-else>1</template></span>
          <span class="lbl">Source</span>
        </button>
      </li>
      <li :class="{ current: step === 2, done: step > 2, locked: !repo }">
        <button type="button" class="step st" aria-label="Step 2: Build and settings" :aria-current="step === 2 ? 'step' : undefined" :disabled="!repo" @click="go(2)">
          <span><Icon v-if="step > 2" name="check" :size="18" /><template v-else>2</template></span>
          <span class="lbl">Build and settings</span>
        </button>
      </li>
      <li :class="{ current: step === 3, locked: !repo }">
        <button type="button" class="step st" aria-label="Step 3: Deploy" :aria-current="step === 3 ? 'step' : undefined" :disabled="!repo" @click="go(3)">
          <span>3</span>
          <span class="lbl">Deploy</span>
        </button>
      </li>
    </ol>

    <Transition name="step" mode="out-in" @after-enter="fitCard">
    <section v-if="step === 1" ref="cardEl" class="card source" :style="{ maxHeight: cardMax ? `${cardMax}px` : undefined }">
        <h2>{{ mode === 'github' ? 'Pick a repository' : 'Public repository' }}</h2>
        <div class="modes" role="tablist" aria-label="Repository source">
          <button type="button" role="tab" class="mode st" :class="{ on: mode === 'github' }" :aria-selected="mode === 'github'" :disabled="!gh?.connected" @click="mode = 'github'">
            <Icon name="github" :size="18" /> GitHub App
          </button>
          <button type="button" role="tab" class="mode st" :class="{ on: mode === 'public' }" :aria-selected="mode === 'public'" @click="mode = 'public'">
            Public URL
          </button>
        </div>

        <template v-if="mode === 'github'">
          <div class="filters">
            <label class="field search">
              <Icon name="search" :size="20" />
              <input v-model="query" placeholder="Search repositories" aria-label="Search repositories" />
            </label>
            <M3Select v-if="accountOptions.length > 2" v-model="account" class="account" label="Account" :options="accountOptions" />
          </div>
          <p v-if="gh?.app?.installUrl" class="muted small install">
            Missing a repository? <a :href="gh.app.installUrl" target="_blank" rel="noopener">Choose repositories and organizations on GitHub</a>, then reload.
          </p>
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
        </template>

        <template v-else>
          <p class="muted small url-hint">
            Paste the URL of any public GitHub repository, like <code>https://github.com/navidrome/navidrome</code>. Nothing has to be installed on GitHub.
          </p>
          <label class="field public-url">
            <Icon name="github" :size="20" />
            <input
              v-model="publicUrl"
              placeholder="https://github.com/owner/repository"
              aria-label="Public GitHub repository URL"
              spellcheck="false"
              autocomplete="off"
              @keyup.enter="usePublic"
            />
          </label>
          <p v-if="publicError" class="err small">{{ publicError }}</p>
          <p v-else class="muted small">
            A public repository cannot notify this server when you push, so automatic deploys stay off. Use Redeploy on the app after a push.
          </p>
          <div class="actions">
            <button v-if="repo && isExternal" class="btn btn-text btn-lg st" type="button" @click="step = 2">
              Keep {{ repo.fullName }}
            </button>
            <button class="btn btn-filled btn-lg st" type="button" :disabled="resolving || !publicUrl.trim()" @click="usePublic">
              <ShapeLoader v-if="resolving" small label="Checking repository" />
              {{ resolving ? 'Checking…' : 'Continue' }}
            </button>
          </div>
        </template>
    </section>

      <section v-else-if="step === 2 && repo" ref="cardEl" class="card config" :style="{ maxHeight: cardMax ? `${cardMax}px` : undefined }">
        <div class="cfg-head">
          <M3Shape kind="cookie" :size="44" fill="var(--pc)" />
          <div class="grow">
            <h2>{{ repo.fullName.split('/')[1] }}</h2>
            <div class="muted small">{{ repo.fullName }}{{ isExternal ? ' · public repository' : repo.private ? ' · private' : '' }}</div>
          </div>
          <button class="btn btn-text st" type="button" @click="step = 1">Change</button>
        </div>

        <div class="group">
          <h3>App</h3>
          <p class="hint">The name it gets here, the branch to deploy and the folder to build.</p>
          <div class="grid">
            <div class="ffield">
              <label class="tf">
                <span>Name</span>
                <input :value="name" spellcheck="false" @input="name = slugify(($event.target as HTMLInputElement).value)" />
              </label>
              <small>Also the container name</small>
            </div>
            <M3Select v-model="branch" label="Branch" :options="branchOptions" />
            <div class="ffield">
              <label class="tf">
                <span>Root folder</span>
                <input v-model.trim="rootDir" placeholder="/" spellcheck="false" />
              </label>
              <small>For a monorepo, the folder of this app</small>
            </div>
          </div>
        </div>

        <div class="group">
          <h3>Access</h3>
          <p class="hint">How the app is reached from outside the server.</p>
          <AccessPicker v-model:access="access" v-model:domain="domain" v-model:allow="allow" />
        </div>

        <div class="group">
          <h3>Build</h3>
          <p class="hint">What turns the repository into an image, and how much it may use.</p>
          <div class="tile">
            <M3Shape kind="clover" :size="36" fill="var(--secc)" />
            <div class="grow">
              <div class="tile-title">{{ detected }}</div>
              <div v-if="detection?.reason" class="muted small">{{ detection.reason }}</div>
            </div>
            <M3Select v-model="build" class="build-sel" label="Build" :options="buildOptions" />
          </div>
          <div class="grid">
            <div class="ffield">
              <label class="tf">
                <span>Port</span>
                <input v-model.number="port" inputmode="numeric" :class="{ warn: needsPort }" />
              </label>
              <small>{{ effectiveBuild === 'compose' ? 'The port your compose file publishes on 127.0.0.1' : needsPort ? 'Required: the Dockerfile has no EXPOSE line' : 'The port the app listens on inside the container' }}</small>
            </div>
            <div class="ffield">
              <label class="tf">
                <span>Memory limit (MiB)</span>
                <input v-model.number="memoryMb" inputmode="numeric" placeholder="0" />
              </label>
              <small>0 for no limit</small>
            </div>
          </div>
          <BuildCommands v-model="cmds" :build="effectiveBuild" />
        </div>

        <div class="group">
          <h3>Environment variables</h3>
          <p class="hint">Values passed to the app when it starts. Secrets stay hidden in the panel.</p>
          <EnvEditor v-model="env" />
          <EnvTarget v-if="effectiveBuild === 'compose'" v-model:file="envFile" v-model:path="envPath" />
        </div>

        <div class="group">
          <h3>Deploy</h3>
          <p class="hint">What happens after each push to the branch.</p>
          <div class="setting">
            <div class="grow">
              <div>Deploy on every push</div>
              <div class="muted small">
                {{ isExternal ? 'Public repositories cannot notify the server: use Redeploy after a push' : 'GitHub notifies the server; the new version replaces the old one without downtime' }}
              </div>
            </div>
            <div class="ctl"><M3Switch v-model="autoDeploy" label="Deploy on every push" :disabled="isExternal" /></div>
          </div>
          <div class="setting">
            <div class="grow">
              <div>Versions kept for rollback</div>
              <div class="muted small">Older images are removed after each deploy</div>
            </div>
            <div class="ctl"><input v-model.number="keep" class="num" inputmode="numeric" aria-label="Versions kept" /></div>
          </div>
          <div class="actions">
            <button class="btn btn-text btn-lg st" type="button" @click="step = 1">Back</button>
            <button class="btn btn-filled btn-lg st" type="button" :disabled="invalid" @click="step = 3">Continue</button>
          </div>
        </div>
      </section>

      <section v-else-if="step === 3 && repo" ref="cardEl" class="card config review-card" :style="{ maxHeight: cardMax ? `${cardMax}px` : undefined }">
        <div class="cfg-head">
          <M3Shape kind="cookie" :size="44" fill="var(--pc)" />
          <div class="grow">
            <h2>Ready to deploy</h2>
            <div class="muted small">{{ repo.fullName }}{{ isExternal ? ' · public repository' : repo.private ? ' · private' : '' }}</div>
          </div>
        </div>

        <dl class="review">
          <div><dt>Name</dt><dd>{{ name }}</dd></div>
          <div><dt>Branch</dt><dd>{{ branch }}</dd></div>
          <div><dt>Root folder</dt><dd>{{ rootDir || '/' }}</dd></div>
          <div><dt>Access</dt><dd>{{ accessText }}</dd></div>
          <div><dt>Build</dt><dd>{{ BUILD_LABEL[effectiveBuild] ?? (detecting ? 'Detecting…' : 'Not detected') }}</dd></div>
          <div><dt>Port</dt><dd>{{ port || 'None' }}</dd></div>
          <div><dt>Memory limit</dt><dd>{{ memoryMb ? `${memoryMb} MiB` : 'No limit' }}</dd></div>
          <div><dt>Environment</dt><dd>{{ envCount ? `${envCount} variable${envCount > 1 ? 's' : ''}` : 'None' }}{{ envFile ? ` · ${envPath || '.env'} file` : '' }}</dd></div>
          <div><dt>Auto-deploy</dt><dd>{{ isExternal ? 'Off (public repository)' : autoDeploy ? 'On every push' : 'Off' }}</dd></div>
          <div><dt>Versions kept</dt><dd>{{ keep }}</dd></div>
        </dl>

        <div class="notes">
          <p v-if="detection?.reason" class="muted small">{{ detection.reason }}</p>
          <p class="muted small">The app is created and its first deployment starts immediately.</p>
        </div>

        <div class="actions">
          <button class="btn btn-text btn-lg st" type="button" @click="step = 2">Back</button>
          <button class="btn btn-filled btn-lg st" type="button" :disabled="saving || invalid" @click="submit">
            <ShapeLoader v-if="saving" small label="Creating app" />
            {{ saving ? 'Creating…' : 'Deploy' }}
          </button>
        </div>
      </section>
    </Transition>
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
  transition:
    background-color 0.2s var(--ease),
    color 0.2s var(--ease),
    opacity 0.2s var(--ease);
}
.steps li.locked {
  opacity: 0.5;
}
.step {
  display: flex;
  align-items: center;
  height: 44px;
  padding: 0 8px;
  border: none;
  border-radius: 22px;
  background: none;
  color: inherit;
  font: inherit;
  font-weight: 500;
  transition: padding-right 0.24s var(--ease);
}
.step:disabled {
  cursor: default;
}
/* Inactive steps show only their number */
.lbl {
  max-width: 0;
  margin-left: 0;
  overflow: hidden;
  white-space: nowrap;
  opacity: 0;
  transition:
    max-width 0.24s var(--ease),
    margin-left 0.24s var(--ease),
    opacity 0.16s var(--ease);
}
.steps li.current .lbl {
  max-width: 240px;
  margin-left: 10px;
  opacity: 1;
}
.steps li.current .step {
  padding-right: 18px;
}
.steps span:not(.lbl) {
  width: 28px;
  height: 28px;
  border-radius: 14px;
  display: grid;
  place-items: center;
  background: var(--outline-v);
  font-size: 13px;
  font-weight: 700;
  transition:
    background-color 0.2s var(--ease),
    color 0.2s var(--ease);
}
/* Step transition: short fade and rise */
.step-enter-active {
  transition:
    opacity 0.16s var(--ease-decel),
    translate 0.22s var(--ease-decel);
}
.step-leave-active {
  transition:
    opacity 0.1s var(--ease),
    translate 0.1s var(--ease);
}
.step-enter-from {
  opacity: 0;
  translate: 0 8px;
}
.step-leave-to {
  opacity: 0;
  translate: 0 -4px;
}
.steps li.current {
  background: var(--pc);
  color: var(--on-pc);
}
.steps li.current span:not(.lbl),
.steps li.done span:not(.lbl) {
  background: var(--primary);
  color: var(--on-primary);
}
.source {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 14px;
}
.source,
.config {
  overflow: auto;
}
.source > *,
.config > * {
  flex-shrink: 0;
}
.source h2,
.config h2 {
  font-size: 20px;
}
.modes {
  display: flex;
  gap: 6px;
}
.mode {
  display: flex;
  align-items: center;
  gap: 8px;
  height: 40px;
  padding: 0 16px;
  border: 1px solid var(--outline);
  border-radius: 20px;
  background: transparent;
  color: var(--on-surface);
  font: inherit;
  font-size: 14px;
  font-weight: 500;
}
.mode.on {
  border-color: var(--primary);
  background: var(--pc);
  color: var(--on-pc);
}
.mode:disabled {
  opacity: 0.45;
}
.public-url {
  width: 100%;
}
.url-hint {
  margin: 0;
}
.filters {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 10px;
}
.filters .search {
  flex: 1;
  min-width: 0;
}
.account {
  flex: none;
  width: 220px;
}
.install {
  margin: -6px 0 0;
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
  align-items: start;
  gap: 12px;
}
/* Same height with or without a hint; the hint sits under the box. */
.ffield {
  display: flex;
  flex-direction: column;
  gap: 6px;
  min-width: 0;
}
.ffield small {
  padding-left: 16px;
  font-size: 12px;
  line-height: 16px;
  color: var(--on-sv);
}
.tf {
  box-sizing: border-box;
  display: flex;
  flex-direction: column;
  justify-content: center;
  gap: 2px;
  height: 56px;
  padding: 6px 16px;
  border-radius: 12px 12px 4px 4px;
  background: var(--schh);
  border-bottom: 1px solid var(--on-sv);
  overflow: hidden;
}
.tf:focus-within {
  border-bottom-color: var(--primary);
  box-shadow: inset 0 -1px 0 var(--primary);
}
.tf span {
  font-size: 12px;
  line-height: 16px;
  font-weight: 500;
  color: var(--primary);
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
}
.tf input {
  height: 24px;
  border: none;
  background: none;
  outline: none;
  font-size: 16px;
  color: var(--on-surface);
  padding: 0;
}
.tf input.warn {
  color: var(--warn);
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
  width: 100%;
  text-align: center;
}
.group {
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.group h3 {
  font-size: 16px;
}
.group .hint {
  margin: -8px 0 0;
  font-size: 13px;
  color: var(--on-sv);
}
.setting {
  display: flex;
  align-items: center;
  gap: 16px;
}
.ctl {
  flex: none;
  display: flex;
  align-items: center;
  justify-content: flex-end;
  width: 80px;
  min-height: 40px;
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
.notes {
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.notes p {
  margin: 0;
}
.actions {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 8px;
}
</style>
