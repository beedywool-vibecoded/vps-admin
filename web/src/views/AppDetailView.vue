<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import Icon from '@/components/Icon.vue'
import M3Shape from '@/components/M3Shape.vue'
import M3Switch from '@/components/M3Switch.vue'
import StatusDot from '@/components/StatusDot.vue'
import EnvEditor from '@/components/EnvEditor.vue'
import M3Select from '@/components/M3Select.vue'
import M3Checkbox from '@/components/M3Checkbox.vue'
import BuildCommands from '@/components/BuildCommands.vue'
import { api, streamDeployment, type AppView, type BuildType, type Deployment, type EnvVar, type UpdateCheck } from '@/lib/api'
import { BUILD_LABEL, STATUS_LABEL, appTone, depTone } from '@/lib/deploys'
import { ago, dateTime } from '@/lib/format'
import { confirm, toast, toastError } from '@/lib/ui'

const props = defineProps<{ id: string }>()
const route = useRoute()
const router = useRouter()

const app = ref<AppView | null>(null)
const deployments = ref<Deployment[]>([])
const env = ref<EnvVar[]>([])
const domains = ref(false)
const tab = ref<'deployments' | 'env' | 'settings'>('deployments')
const busy = ref(false)

// Selected deployment and its build output
const selected = ref<number | null>(null)
const log = ref('')
const logEl = ref<HTMLElement>()
let closeStream: (() => void) | undefined

// Settings form
const form = ref({ branch: '', rootDir: '', build: 'auto' as BuildType, port: 0, domain: '', autoDeploy: true, keep: 5, memoryMb: 0 })
const cmds = ref({ installCmd: '', buildCmd: '', startCmd: '', dockerfile: '', composeFile: '' })
const checkResult = ref<UpdateCheck | null>(null)
const checking = ref(false)
const effectiveBuild = computed(() => (form.value.build === 'auto' ? app.value?.live?.build || app.value?.latest?.build || '' : form.value.build))
const deleteVolumes = ref(false)
const buildOptions = (Object.keys(BUILD_LABEL) as BuildType[]).map((k) => ({ value: k, label: BUILD_LABEL[k]! }))

const running = computed(() => deployments.value.some((d) => ['queued', 'building', 'starting'].includes(d.status)))
const selectedDep = computed(() => deployments.value.find((d) => d.id === selected.value))

async function load() {
  try {
    const res = await api.app(props.id)
    app.value = res.app
    deployments.value = res.deployments
    domains.value = res.domains
    if (!envDirty) env.value = res.env.map((e) => ({ ...e, keep: e.secret && e.hasValue }))
    const a = res.app
    if (!formDirty) {
      form.value = { branch: a.branch, rootDir: a.rootDir, build: a.build, port: a.port, domain: a.domain, autoDeploy: a.autoDeploy, keep: a.keep, memoryMb: a.memoryMb }
      cmds.value = { installCmd: a.installCmd, buildCmd: a.buildCmd, startCmd: a.startCmd, dockerfile: a.dockerfile, composeFile: a.composeFile }
    }
    if (selected.value === null && res.deployments[0]) select(res.deployments[0].id)
  } catch (e) {
    toastError(e)
  }
}

let envDirty = false
let formDirty = false
watch(env, () => (envDirty = true), { deep: true })
watch([form, cmds], () => (formDirty = true), { deep: true })

function select(id: number) {
  selected.value = id
  closeStream?.()
  log.value = ''
  closeStream = streamDeployment(props.id, id, {
    log: async (chunk) => {
      const el = logEl.value
      const atBottom = !el || el.scrollHeight - el.scrollTop - el.clientHeight < 60
      log.value += chunk
      if (atBottom) {
        await nextTick()
        logEl.value?.scrollTo({ top: logEl.value.scrollHeight })
      }
    },
    status: (d) => {
      const i = deployments.value.findIndex((x) => x.id === d.id)
      if (i >= 0) deployments.value[i] = d
    },
    end: () => load(),
  })
}

async function action(fn: () => Promise<unknown>, done: string) {
  busy.value = true
  try {
    const res = await fn()
    toast(done)
    await load()
    if (res && typeof res === 'object' && 'id' in res && typeof res.id === 'number') select(res.id)
  } catch (e) {
    toastError(e)
  } finally {
    busy.value = false
  }
}

const redeploy = () => action(() => api.deploy(props.id), 'Deployment started')
const restart = () => action(() => api.restartApp(props.id), 'Restarted')
async function rollback(d: Deployment) {
  const ok = await confirm({
    title: `Roll back to ${d.sha.slice(0, 7)}?`,
    body: `"${d.message}" goes live again${app.value?.build === 'compose' ? ' (rebuilt from that commit)' : ', without rebuilding'}. Automatic deploys stay on: the next push deploys again.`,
    action: 'Roll back',
  })
  if (ok) action(() => api.rollback(props.id, d.id), 'Rollback started')
}

async function saveEnv() {
  busy.value = true
  try {
    await api.setEnv(props.id, env.value)
    envDirty = false
    await load()
    const ok = await confirm({ title: 'Variables saved', body: 'They apply at the next deployment. Redeploy now?', action: 'Redeploy' })
    if (ok) redeploy()
  } catch (e) {
    toastError(e)
  } finally {
    busy.value = false
  }
}

async function checkUpdates() {
  checking.value = true
  try {
    checkResult.value = await api.checkApp(props.id)
  } catch (e) {
    toastError(e)
  } finally {
    checking.value = false
  }
}

async function saveSettings() {
  busy.value = true
  try {
    await api.updateApp(props.id, { ...form.value, ...cmds.value })
    formDirty = false
    toast('Settings saved, they apply at the next deployment')
    await load()
  } catch (e) {
    toastError(e)
  } finally {
    busy.value = false
  }
}

async function remove() {
  const ok = await confirm({
    title: app.value?.dir ? `Unlink ${props.id}?` : `Delete ${props.id}?`,
    body: app.value?.dir
      ? `Pushes stop updating ${app.value.dir}. The folder and its containers stay as they are.`
      : `Stops and removes its containers, images, Caddy site and files${deleteVolumes.value ? ', and its data volumes' : '. Named volumes are kept'}. The repository is not touched.`,
    action: app.value?.dir ? 'Unlink' : 'Delete',
    danger: !app.value?.dir,
  })
  if (!ok) return
  busy.value = true
  try {
    await api.deleteApp(props.id, deleteVolumes.value)
    toast(`${props.id} ${app.value?.dir ? 'unlinked' : 'deleted'}`)
    router.replace('/apps')
  } catch (e) {
    toastError(e)
    busy.value = false
  }
}

let timer: ReturnType<typeof setInterval> | undefined
onMounted(async () => {
  const dep = Number(route.query.dep)
  if (dep) select(dep)
  await load()
  timer = setInterval(() => !running.value || load(), 5000)
})
onBeforeUnmount(() => {
  clearInterval(timer)
  closeStream?.()
})
</script>

<template>
  <div v-if="app" class="page">
    <header class="head">
      <RouterLink to="/apps" class="icon-btn st" aria-label="Back to apps"><Icon name="back" /></RouterLink>
      <M3Shape kind="cookie" :size="52" fill="var(--pc)" />
      <div class="grow">
        <h1>{{ app.id }}</h1>
        <div class="meta small muted">
          <span class="state"><StatusDot :tone="appTone(app.status, app.latest)" :label="app.status" /> {{ running ? 'Deploying' : app.status }}</span>
          <a v-if="app.domain" :href="`https://${app.domain}`" target="_blank" rel="noopener">{{ app.domain }}</a>
          <span><Icon name="github" :size="14" /> {{ app.repo }} · <Icon name="branch" :size="14" /> {{ app.branch }}</span>
          <span>{{ BUILD_LABEL[app.live?.build || app.latest?.build || app.build] }}</span>
          <span v-if="app.live?.hostPort">127.0.0.1:{{ app.live.hostPort }}</span>
          <span v-if="app.dir"><Icon name="folder" :size="14" /> <code>{{ app.dir }}</code></span>
        </div>
      </div>
      <button class="btn btn-text st" type="button" :disabled="checking" @click="checkUpdates">{{ checking ? 'Checking…' : 'Check for updates' }}</button>
      <button class="btn btn-outlined st" type="button" :disabled="busy || !app.live" @click="restart">Restart</button>
      <button class="btn btn-filled st" type="button" :disabled="busy || running" @click="redeploy">Redeploy</button>
    </header>

    <div v-if="checkResult" class="upd card" :class="{ fresh: !checkResult.upToDate }">
      <Icon :name="checkResult.upToDate ? 'check' : 'updates'" :size="20" />
      <div class="grow small">
        <template v-if="checkResult.upToDate">Up to date with {{ app.branch }}: <code>{{ checkResult.current.slice(0, 7) }}</code></template>
        <template v-else>
          New commit on {{ app.branch }}: <code>{{ checkResult.latest.sha.slice(0, 7) }}</code> {{ checkResult.latest.message }}
          <span class="muted">(running <code>{{ checkResult.current.slice(0, 7) || 'nothing' }}</code>)</span>
        </template>
      </div>
      <button v-if="!checkResult.upToDate" class="btn btn-filled st" type="button" :disabled="busy || running" @click="redeploy(); checkResult = null">Update now</button>
      <button class="icon-btn st" type="button" aria-label="Dismiss" @click="checkResult = null"><Icon name="close" :size="18" /></button>
    </div>

    <div class="tabs" role="tablist" aria-label="App sections">
      <button v-for="t in (['deployments', 'env', 'settings'] as const)" :key="t" role="tab" type="button" class="st" :class="{ on: tab === t }" :aria-selected="tab === t" @click="tab = t">
        {{ t === 'deployments' ? 'Deployments' : t === 'env' ? 'Environment' : 'Settings' }}
      </button>
    </div>

    <div v-if="tab === 'deployments'" class="deps">
      <section class="card list">
        <div class="card-head">
          <h2>Deployments</h2>
          <span v-if="app.autoDeploy" class="chip">Auto-deploy on push</span>
        </div>
        <p v-if="!deployments.length" class="muted">No deployment yet.</p>
        <div v-for="d in deployments" :key="d.id" class="dep" :class="{ sel: d.id === selected }">
          <button type="button" class="dep-main st" :aria-pressed="d.id === selected" @click="select(d.id)">
            <span class="status" :class="d.status"><StatusDot :tone="depTone(d)" :label="d.status" /> {{ STATUS_LABEL[d.status] }}</span>
            <code>{{ d.sha.slice(0, 7) || '…' }}</code>
            <span class="msg ellipsis">{{ d.message || d.trigger }}</span>
            <span class="when muted small" v-tip="dateTime(d.createdAt)">{{ ago(d.createdAt) }}</span>
          </button>
          <button v-if="d.status === 'superseded'" type="button" class="btn btn-outlined st rb" :disabled="busy || running" @click="rollback(d)">Roll back</button>
        </div>
      </section>

      <section class="card logcard">
        <div class="card-head">
          <h2>Build log<template v-if="selectedDep"> · {{ selectedDep.sha.slice(0, 7) }}</template></h2>
          <span v-if="selectedDep" class="muted small">{{ selectedDep.trigger }} · {{ dateTime(selectedDep.createdAt) }}</span>
        </div>
        <div v-if="selectedDep?.error" class="err small"><Icon name="alert" :size="16" /> {{ selectedDep.error }}</div>
        <pre ref="logEl" class="log">{{ log || (selected ? 'Waiting for output…' : 'Select a deployment') }}</pre>
      </section>
    </div>

    <section v-else-if="tab === 'env'" class="card stack">
      <div class="card-head">
        <h2>Environment variables</h2>
        <span class="muted small">Encrypted on the server · secrets are never sent back to the panel</span>
      </div>
      <EnvEditor v-model="env" />
      <div class="actions">
        <button class="btn btn-filled st" type="button" :disabled="busy" @click="saveEnv">Save</button>
      </div>
    </section>

    <template v-else>
      <section class="card stack">
        <div class="card-head"><h2>Settings</h2></div>
        <div class="grid">
          <label class="tf"><span>Branch</span><input v-model.trim="form.branch" /></label>
          <label class="tf"><span>Root folder</span><input v-model.trim="form.rootDir" placeholder="/" /></label>
          <label class="tf"><span>Domain</span><input v-model.trim="form.domain" :disabled="!domains" :placeholder="domains ? 'app.example.com' : 'needs Caddy'" /></label>
          <label class="tf"><span>Port</span><input v-model.number="form.port" inputmode="numeric" /></label>
          <template v-if="!app.dir">
            <label class="tf"><span>Memory limit (MiB, 0 for none)</span><input v-model.number="form.memoryMb" inputmode="numeric" /></label>
            <label class="tf"><span>Versions kept</span><input v-model.number="form.keep" inputmode="numeric" /></label>
          </template>
          <M3Select v-if="!app.dir" v-model="form.build" label="Build" :options="buildOptions" />
        </div>
        <BuildCommands v-model="cmds" :build="app.dir ? 'compose' : effectiveBuild" />
        <div class="setting">
          <div class="grow">
            <div>{{ app.dir ? 'Update the folder on every push' : 'Deploy on every push' }}</div>
            <div class="muted small">On {{ form.branch }}</div>
          </div>
          <M3Switch v-model="form.autoDeploy" label="Deploy on every push" />
        </div>
        <div class="actions">
          <button class="btn btn-filled st" type="button" :disabled="busy" @click="saveSettings">Save</button>
        </div>
      </section>

      <section v-if="app.dir" class="card danger stack">
        <h2>Unlink from GitHub</h2>
        <p class="muted small">Pushes stop updating <code>{{ app.dir }}</code>. Nothing is deleted: the folder and its containers keep running.</p>
        <button class="btn btn-outlined st" type="button" :disabled="busy || running" @click="remove">Unlink {{ app.id }}</button>
      </section>
      <section v-else class="card danger stack">
        <h2>Delete app</h2>
        <p class="muted small">Stops and removes the containers, images, the Caddy site and the source folder. Named volumes are kept unless you tick the box.</p>
        <M3Checkbox v-model="deleteVolumes" class="check" label="Also delete its data volumes" />
        <button class="btn btn-danger st" type="button" :disabled="busy || running" @click="remove">Delete {{ app.id }}</button>
      </section>
    </template>
  </div>
  <p v-else class="muted">Loading…</p>
</template>

<style scoped>
.upd {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 12px;
  padding: 12px 16px;
}
.upd.fresh {
  background: var(--tc);
  color: var(--on-tc);
}
.head {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 14px;
}
.head h1 {
  font-size: 30px;
}
.meta {
  display: flex;
  flex-wrap: wrap;
  gap: 6px 14px;
  margin-top: 4px;
}
.meta span,
.state {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}
.meta a {
  text-decoration: none;
}
.tabs {
  display: flex;
  gap: 4px;
  border-bottom: 1px solid var(--outline-v);
}
.tabs button {
  height: 48px;
  padding: 0 18px;
  border: none;
  background: transparent;
  color: var(--on-sv);
  font-size: 15px;
  font-weight: 500;
  border-radius: 12px 12px 0 0;
}
.tabs button.on {
  color: var(--primary);
  box-shadow: inset 0 -3px 0 var(--primary);
}
.deps {
  display: flex;
  flex-wrap: wrap;
  gap: 16px;
  align-items: flex-start;
}
.list {
  flex: 1 1 min(460px, 100%);
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.dep {
  display: flex;
  align-items: center;
  gap: 8px;
  padding-right: 8px;
  border-radius: 14px;
}
.dep-main {
  flex: 1;
  min-width: 0;
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 10px 12px;
  border: none;
  border-radius: 14px;
  background: transparent;
  color: var(--on-surface);
  text-align: left;
  font-size: 14px;
}
.dep.sel {
  background: var(--sch);
}
.status {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  width: 104px;
  flex-shrink: 0;
  font-size: 13px;
  font-weight: 500;
}
.dep code {
  color: var(--primary);
  font-size: 13px;
}
.msg {
  flex: 1;
  min-width: 0;
}
.when {
  width: 96px;
  text-align: right;
  flex-shrink: 0;
}
.rb {
  height: 32px;
  padding: 0 12px;
  font-size: 13px;
}
.logcard {
  flex: 1.3 1 min(520px, 100%);
  display: flex;
  flex-direction: column;
  gap: 10px;
  min-width: 0;
}
.log {
  margin: 0;
  height: 520px;
  overflow: auto;
  padding: 14px 16px;
  border-radius: 16px;
  background: var(--scc);
  border: 1px solid var(--sch);
  font-size: 12px;
  line-height: 1.6;
  white-space: pre-wrap;
  word-break: break-all;
}
.err {
  display: flex;
  gap: 8px;
  padding: 10px 12px;
  border-radius: 12px;
  background: var(--ec);
  color: var(--on-ec);
}
.stack {
  display: flex;
  flex-direction: column;
  gap: 14px;
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
  background: var(--sch);
  border-bottom: 2px solid var(--on-sv);
}
.tf span {
  font-size: 12px;
  font-weight: 500;
  color: var(--primary);
}
.tf input,
.tf select {
  border: none;
  background: none;
  outline: none;
  font-size: 16px;
  color: var(--on-surface);
  padding: 2px 0;
}
.tf select option {
  background: var(--sch);
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
.danger {
  border: 1px solid var(--ec);
  background: transparent;
}
.danger h2 {
  color: var(--error);
  font-size: 20px;
}
.danger p {
  margin: 0;
}
.danger .btn {
  align-self: flex-start;
}
.check {
  display: flex;
  align-items: center;
  gap: 8px;
}
@media (max-width: 760px) {
  .when,
  .status {
    width: auto;
  }
  .log {
    height: 360px;
  }
}
</style>
