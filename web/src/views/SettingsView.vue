<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import BackupCard from '@/components/BackupCard.vue'
import Icon from '@/components/Icon.vue'
import M3Slider from '@/components/M3Slider.vue'
import M3Switch from '@/components/M3Switch.vue'
import M3Dialog from '@/components/M3Dialog.vue'
import { appearance, loadAppearance, removeBackground, setOpacity, uploadBackground } from '@/lib/appearance'
import { api, type AlertSettings, type AuditEntry, type Device, type ServerSetup, type Tool } from '@/lib/api'
import ShapeLoader from '@/components/ShapeLoader.vue'
import StatusDot from '@/components/StatusDot.vue'
import { passkeysSupported, registerPasskey } from '@/lib/passkey'
import { SEEDS, mode, seed, type ThemeMode } from '@/lib/theme'
import { ago, dateTime } from '@/lib/format'
import { confirm, toast, toastError } from '@/lib/ui'
import { resetAuthCheck } from '@/router'

const router = useRouter()
const devices = ref<Device[]>([])
const current = ref('')
const audit = ref<AuditEntry[]>([])
const moreAudit = ref(true)
// Activity: the latest few, the rest on demand in a scrolling list.
const auditOpen = ref(false)
const shownAudit = computed(() => (auditOpen.value ? audit.value : audit.value.slice(0, 5)))
const loadingAudit = ref(false)
async function olderAudit() {
  loadingAudit.value = true
  try {
    await loadAudit(true)
  } catch (e) {
    toastError(e)
  } finally {
    loadingAudit.value = false
  }
}
const addingKey = ref(false)
const fileInput = ref<HTMLInputElement>()
const uploading = ref(false)
const opacity = ref(appearance.opacity)
const alertSet = ref<AlertSettings | null>(null)
const alertWebhook = ref('')
const alertDisk = ref(90)
const alertTesting = ref(false)
const alertEnabled = computed({
  get: () => alertSet.value?.enabled ?? false,
  set: (v: boolean) => saveAlerts({ enabled: v }),
})
const diskLimit = () => Math.min(99, Math.max(50, Math.round(alertDisk.value || 90)))
const setup = ref<ServerSetup | null>(null)
const setupError = ref(false)
const proxyTools = computed(() =>
  setup.value
    ? [
        { name: 'Caddy', tool: setup.value.environment.caddy },
        { name: 'nginx', tool: setup.value.environment.nginx },
        { name: setup.value.environment.certbot.nginxPlugin ? 'certbot (nginx plugin)' : 'certbot', tool: setup.value.environment.certbot },
      ]
    : [],
)
// One value per tool: what it is doing now, and its release when it has one.
function toolValue(t: Tool): string {
  if (!t.installed) return 'not installed'
  return [t.running ? '' : 'stopped', t.version ?? ''].filter(Boolean).join(' · ') || 'installed'
}
// The proxy that actually serves the apps, if any is up.
const activeProxy = computed(() => {
  const e = setup.value?.environment
  if (!e) return ''
  if (e.caddy.installed && e.caddy.running) return 'Caddy'
  if (e.nginx.installed && e.nginx.running) return 'nginx'
  if (e.traefik) return 'Traefik'
  return ''
})
const accessLabel = computed(() => {
  const a = setup.value?.panel.access
  return a === 'tailscale' ? 'Tailscale only' : a === 'local' ? 'Local' : 'Public'
})
const domains = computed(() => {
  const e = setup.value?.environment
  if (!e) return { ok: false, title: '', detail: '' }
  if (!setup.value?.sites) {
    return { ok: false, title: 'Not configured', detail: 'Rerun the installer with --domain to publish apps on HTTPS.' }
  }
  const caddy = e.caddy.installed && e.caddy.running
  return {
    ok: true,
    title: 'HTTPS ready',
    detail: caddy ? 'Served by Caddy, which issues its own certificates' : 'Served by nginx, certificates by certbot',
  }
})
const networkUp = computed(() => networks.value.filter((n) => n.tone === 'ok').length)
// One row per private network, so the tile reads the same whether none or several are up.
const networks = computed(() => {
  const e = setup.value?.environment
  if (!e) return []
  const rows: { name: string; detail: string; tone: 'ok' | 'warn' | 'idle' }[] = []
  if (e.tailscale) {
    rows.push({
      name: 'Tailscale',
      detail: e.tailscale.running ? `${e.tailscale.name} · HTTPS ${e.tailscale.https ? 'on' : 'off'}` : 'installed, stopped',
      tone: e.tailscale.running ? 'ok' : 'warn',
    })
  } else {
    rows.push({ name: 'Tailscale', detail: 'not installed', tone: 'idle' })
  }
  for (const l of e.wireguard) rows.push({ name: `WireGuard ${l.name}`, detail: l.addresses.join(', '), tone: 'ok' })
  for (const l of e.zerotier) rows.push({ name: `ZeroTier ${l.name}`, detail: l.addresses.join(', '), tone: 'ok' })
  if (!e.wireguard.length && !e.zerotier.length) rows.push({ name: 'WireGuard, ZeroTier', detail: 'none linked', tone: 'idle' })
  return rows
})

async function pickBackground(e: Event) {
  const file = (e.target as HTMLInputElement).files?.[0]
  ;(e.target as HTMLInputElement).value = ''
  if (!file) return
  uploading.value = true
  try {
    await uploadBackground(file)
    toast('Background updated for everyone')
  } catch (err) {
    toastError(err)
  } finally {
    uploading.value = false
  }
}
async function commitOpacity(v: number) {
  try {
    await setOpacity(v)
  } catch (err) {
    toastError(err)
  }
}

// Alerts save themselves, like the appearance above: the switch at once, the URL and
// the threshold when the field is left.
async function loadAlerts() {
  alertSet.value = await api.alerts()
  alertWebhook.value = alertSet.value.webhook
  alertDisk.value = alertSet.value.disk
}
async function saveAlerts(s: { enabled?: boolean; webhook?: string; disk?: number }) {
  try {
    alertSet.value = await api.updateAlerts(s)
    alertWebhook.value = alertSet.value.webhook
    alertDisk.value = alertSet.value.disk
    toast(s.enabled === undefined ? 'Alerts saved' : s.enabled ? 'Alerts on' : 'Alerts off')
  } catch (err) {
    toastError(err)
  }
}
async function testAlerts() {
  alertTesting.value = true
  try {
    await api.testAlerts(alertWebhook.value.trim())
    toast('Test sent: check what received it')
    await loadAlerts()
  } catch (err) {
    toastError(err)
  } finally {
    alertTesting.value = false
  }
}
async function dropBackground() {
  const ok = await confirm({ title: 'Remove the background?', body: 'Everyone gets the plain background again.', action: 'Remove' })
  if (!ok) return
  try {
    await removeBackground()
  } catch (err) {
    toastError(err)
  }
}

const MODES: { id: ThemeMode; label: string }[] = [
  { id: 'system', label: 'System' },
  { id: 'dark', label: 'Dark' },
  { id: 'light', label: 'Light' },
]
// Devices grouped by the person using them, yours first.
const people = computed(() => {
  const groups = new Map<string, Device[]>()
  for (const d of devices.value) {
    const k = d.owner || ''
    groups.set(k, [...(groups.get(k) ?? []), d])
  }
  const me = devices.value.find((d) => d.id === current.value)?.owner ?? ''
  return [...groups.entries()]
    .map(([owner, list]) => ({ owner, list: list.sort((a, b) => (a.id === current.value ? -1 : b.id === current.value ? 1 : 0)) }))
    .sort((a, b) => (a.owner === me ? -1 : b.owner === me ? 1 : !a.owner ? 1 : !b.owner ? -1 : a.owner.localeCompare(b.owner)))
})
const unnamedSelf = computed(() => {
  const d = devices.value.find((x) => x.id === current.value)
  return d && !d.owner ? d : null
})
const initial = (s: string) => (s.trim()[0] ?? '?').toUpperCase()

// Rename dialog
const editing = ref<Device | null>(null)
const editOpen = computed({ get: () => editing.value !== null, set: (v) => !v && (editing.value = null) })
const editOwner = ref('')
const editName = ref('')
function startEdit(d: Device) {
  editing.value = d
  editOwner.value = d.owner
  editName.value = d.name
}
async function saveEdit() {
  const d = editing.value
  if (!d || !editOwner.value.trim() || !editName.value.trim()) return
  try {
    await api.renameDevice(d.id, editOwner.value.trim(), editName.value.trim())
    editing.value = null
    await loadDevices()
  } catch (e) {
    toastError(e)
  }
}
const selfOwner = ref('')
async function nameSelf() {
  const d = unnamedSelf.value
  if (!d || !selfOwner.value.trim()) return
  try {
    await api.renameDevice(d.id, selfOwner.value.trim(), d.name)
    toast('Saved')
    await loadDevices()
  } catch (e) {
    toastError(e)
  }
}

async function loadDevices() {
  const d = await api.devices()
  devices.value = d.devices
  current.value = d.current
}
async function loadAudit(more = false) {
  const before = more ? (audit.value[audit.value.length - 1]?.id ?? 0) : 0
  const { entries } = await api.audit(before)
  audit.value = more ? [...audit.value, ...entries] : entries
  moreAudit.value = entries.length === 50
}

async function revoke(d: Device) {
  const self = d.id === current.value
  const ok = await confirm({
    title: `Remove ${d.owner ? `${d.owner}'s ` : ''}${d.name}?`,
    body: self
      ? 'This is the browser you are using: you will be signed out and need a new pairing code to come back.'
      : `Its ${d.kind === 'app' ? 'token stops working' : 'passkeys and sessions are deleted'} right away.`,
    action: 'Remove',
    danger: true,
  })
  if (!ok) return
  try {
    await api.revokeDevice(d.id)
    if (self) {
      resetAuthCheck()
      return router.replace('/login')
    }
    toast(`${d.name} removed`)
    await loadDevices()
  } catch (e) {
    toastError(e)
  }
}

async function addPasskey() {
  addingKey.value = true
  try {
    await registerPasskey()
    toast('Passkey added')
    await loadDevices()
  } catch (e) {
    if (!(e instanceof DOMException && e.name === 'NotAllowedError')) toastError(e)
  } finally {
    addingKey.value = false
  }
}

async function logout() {
  await api.logout().catch(() => {})
  resetAuthCheck()
  router.replace('/login')
}

const ACTIONS: Record<string, string> = {
  'auth.pair': 'Paired a device',
  'auth.passkey_login': 'Signed in with a passkey',
  'auth.passkey_register': 'Added a passkey',
  'device.revoke': 'Removed a device',
  'device.rename': 'Renamed a device',
  'update.apply': 'Installed an update',
  'update.auto': 'Auto-update',
  'settings.update': 'Changed update settings',
  'settings.alerts': 'Changed alert settings',
  'alert.sent': 'Sent an alert',
  'alert.failed': 'Alert could not be sent',
}
const describe = (e: AuditEntry) => {
  if (ACTIONS[e.action]) return ACTIONS[e.action]
  const [kind, verb] = e.action.split('.')
  return `${verb ? verb.charAt(0).toUpperCase() + verb.slice(1) : e.action} ${kind === 'container' ? 'container' : kind === 'service' ? 'service' : ''}`.trim()
}

onMounted(async () => {
  try {
    api.environment().then((e) => (setup.value = e)).catch(() => (setupError.value = true))
    await Promise.all([loadDevices(), loadAudit(), loadAppearance(), loadAlerts()])
    opacity.value = appearance.opacity
  } catch (e) {
    toastError(e)
  }
})
</script>

<template>
  <div class="page">
    <div class="page-title">
      <h1>Settings</h1>
      <button class="btn btn-outlined st" type="button" @click="logout"><Icon name="logout" small /> Sign out</button>
    </div>

    <section class="card">
      <div class="card-head"><h2>Appearance</h2></div>
      <div class="setting">
        <div class="grow">
          <div>Theme</div>
          <div class="muted small">Saved in this browser</div>
        </div>
        <div class="seg" role="radiogroup" aria-label="Theme">
          <button
            v-for="m in MODES"
            :key="m.id"
            type="button"
            role="radio"
            class="st"
            :class="{ on: mode === m.id }"
            :aria-checked="mode === m.id"
            @click="mode = m.id"
          >
            {{ m.label }}
          </button>
        </div>
      </div>
      <div class="setting col">
        <div>
          <div>Seed color</div>
          <div class="muted small">The whole palette is generated from it, like Material You on Android</div>
        </div>
        <div class="seeds" role="radiogroup" aria-label="Seed color">
          <button
            v-for="s in SEEDS"
            :key="s.hex"
            type="button"
            role="radio"
            class="seed"
            :class="{ on: seed.toLowerCase() === s.hex.toLowerCase() }"
            :aria-checked="seed.toLowerCase() === s.hex.toLowerCase()"
            :aria-label="s.name"
            v-tip="s.name"
            :style="{ background: s.hex }"
            @click="seed = s.hex"
          />
        </div>
      </div>
    </section>

    <section class="card">
      <div class="card-head">
        <h2>Server background</h2>
        <span class="muted small">Shown to everyone, on the panel and in the app</span>
      </div>
      <div class="bg-row">
        <div class="preview" :class="{ empty: !appearance.background }">
          <img v-if="appearance.background" :src="`/api/appearance/background?v=${appearance.version}`" alt="" :style="{ opacity }" />
          <span v-else class="muted small">Plain</span>
        </div>
        <div class="grow bg-controls">
          <div class="bg-buttons">
            <button class="btn btn-tonal st" type="button" :disabled="uploading" @click="fileInput?.click()">
              <Icon name="plus" small /> {{ appearance.background ? 'Change image' : 'Choose an image' }}
            </button>
            <button v-if="appearance.background" class="btn btn-text st" type="button" @click="dropBackground">Use a plain background</button>
            <input ref="fileInput" class="sr-only" type="file" accept="image/jpeg,image/png,image/webp" tabindex="-1" aria-hidden="true" @change="pickBackground" />
          </div>
          <div v-if="appearance.background">
            <div class="small muted">Opacity</div>
            <M3Slider v-model="opacity" label="Background opacity" :min="0.05" :max="1" :step="0.05" :format="(v) => `${Math.round(v * 100)}%`" @update:model-value="appearance.opacity = $event" @commit="commitOpacity" />
          </div>
          <p class="muted small" style="margin: 0">JPEG, PNG or WebP, up to 8 MB. Cards stay opaque so text keeps its contrast.</p>
        </div>
      </div>
    </section>

    <section class="card">
      <div class="card-head">
        <h2>Alerts</h2>
        <span class="muted small">A webhook for what breaks on this server</span>
      </div>
      <div class="setting">
        <div class="grow">
          <div>Notify me</div>
          <div class="muted small">A container that stops, crashes or turns unhealthy, and a disk that fills up. Deployments and stops from the panel never alert, and every delivery is listed in Activity.</div>
        </div>
        <M3Switch v-model="alertEnabled" label="Send alerts" :disabled="!alertSet" />
      </div>
      <div class="setting col">
        <div>
          <div>Webhook URL</div>
          <div class="muted small">
            Anything that takes a POST: ntfy, Discord, Slack, n8n. The body is JSON with <code>title</code>, <code>body</code>, <code>text</code> and <code>name</code>.
          </div>
        </div>
        <div class="alert-row">
          <label class="field grow">
            <Icon name="bell" :size="20" />
            <input
              v-model="alertWebhook"
              placeholder="https://ntfy.sh/my-topic"
              aria-label="Webhook URL"
              spellcheck="false"
              autocomplete="off"
              @change="saveAlerts({ webhook: alertWebhook.trim() })"
            />
          </label>
          <button class="btn btn-tonal st" type="button" :disabled="alertTesting || !alertWebhook.trim()" @click="testAlerts">
            <Icon name="send" small /> {{ alertTesting ? 'Sending…' : 'Send a test' }}
          </button>
        </div>
      </div>
      <div class="setting">
        <div class="grow">
          <div>Disk alert</div>
          <div class="muted small">Warn me when a disk goes above this share</div>
        </div>
        <label class="field alert-disk">
          <input
            v-model.number="alertDisk"
            type="number"
            min="50"
            max="99"
            aria-label="Disk threshold in percent"
            @change="saveAlerts({ disk: diskLimit() })"
          />
          <span class="muted small">%</span>
        </label>
      </div>
      <p v-if="alertSet?.error" class="alert-error muted small">
        <Icon name="alert" :size="16" />
        <span>Last delivery failed: {{ alertSet.error }}{{ alertSet.errorAt ? ` (${ago(new Date(alertSet.errorAt * 1000).toISOString())})` : '' }}</span>
      </p>
    </section>

    <BackupCard />

    <section class="card">
      <div class="card-head">
        <h2>Server setup</h2>
        <span class="muted small">What this server offers to publish apps · detected {{ setup ? ago(setup.environment.checked) : '…' }}</span>
      </div>
      <div v-if="!setup && !setupError" class="setup-grid" aria-busy="true">
        <div v-for="n in 6" :key="n" class="tile skeleton" />
        <span class="sr-only">Looking at the server…</span>
      </div>
      <p v-else-if="setupError" class="muted small">Could not read the server environment.</p>
      <template v-else-if="setup">
        <div class="setup-grid">
          <article class="tile">
            <div class="tile-head">
              <span class="tile-icon primary"><Icon name="globe" :size="20" /></span>
              <h3 class="tile-label">Panel</h3>
              <span class="chip" :class="setup.panel.access === 'public' ? 'chip-outline' : ''">{{ accessLabel }}</span>
            </div>
            <div class="tile-value"><code class="plain">{{ setup.panel.url }}</code></div>
            <div class="tile-sub muted small">
              <template v-if="setup.panel.webhookUrl">GitHub webhooks through <code class="plain">{{ setup.panel.webhookUrl }}</code></template>
              <template v-else>GitHub webhooks arrive on this domain</template>
            </div>
          </article>

          <article class="tile">
            <div class="tile-head">
              <span class="tile-icon tertiary"><Icon name="apps" :size="20" /></span>
              <h3 class="tile-label">App domains</h3>
              <StatusDot :tone="domains.ok ? 'ok' : 'idle'" :label="domains.ok ? 'configured' : 'not configured'" />
            </div>
            <div class="tile-value">{{ domains.title }}</div>
            <div class="tile-sub muted small">{{ domains.detail }}</div>
          </article>

          <article class="tile">
            <div class="tile-head">
              <span class="tile-icon tertiary"><Icon name="security" :size="20" /></span>
              <h3 class="tile-label">Firewall</h3>
              <StatusDot :tone="setup.environment.ufw ? 'ok' : 'idle'" :label="setup.environment.ufw ? setup.environment.ufw : 'not installed'" />
            </div>
            <div class="tile-value">{{ setup.environment.ufw ? `ufw ${setup.environment.ufw}` : 'No firewall' }}</div>
            <div class="tile-sub muted small">nftables {{ setup.environment.nftables ? 'active' : 'not active' }}</div>
          </article>

          <article class="tile">
            <div class="tile-head">
              <span class="tile-icon"><Icon name="server" :size="20" /></span>
              <h3 class="tile-label">Reverse proxy</h3>
              <span v-if="activeProxy" class="chip">{{ activeProxy }}</span>
            </div>
            <div class="tool-list">
              <div v-for="t in proxyTools" :key="t.name" class="tool" :class="{ off: !t.tool.installed }">
                <StatusDot :tone="!t.tool.installed ? 'idle' : t.tool.running ? 'ok' : 'warn'" :label="t.tool.installed ? (t.tool.running ? 'running' : 'stopped') : 'not installed'" />
                <span class="tool-name">{{ t.name }}</span>
                <span class="tool-value muted small" :title="toolValue(t.tool)">{{ toolValue(t.tool) }}</span>
              </div>
              <div v-if="setup.environment.traefik" class="tool">
                <StatusDot tone="ok" label="running" />
                <span class="tool-name">Traefik</span>
                <span class="tool-value muted small">in Docker</span>
              </div>
            </div>
          </article>

          <article class="tile">
            <div class="tile-head">
              <span class="tile-icon primary"><Icon name="lock" :size="20" /></span>
              <h3 class="tile-label">Private networks</h3>
              <span class="chip">{{ networkUp ? `${networkUp} up` : 'none' }}</span>
            </div>
            <div class="tool-list">
              <div v-for="n in networks" :key="n.name" class="tool" :class="{ off: n.tone === 'idle' }">
                <StatusDot :tone="n.tone" :label="n.detail" />
                <span class="tool-name">{{ n.name }}</span>
                <span class="tool-value muted small" :title="n.detail">{{ n.detail }}</span>
              </div>
            </div>
          </article>

          <article class="tile">
            <div class="tile-head">
              <span class="tile-icon"><Icon name="containers" :size="20" /></span>
              <h3 class="tile-label">Docker</h3>
              <StatusDot :tone="setup.environment.docker ? 'ok' : 'idle'" :label="setup.environment.docker ? 'running' : 'not reachable'" />
            </div>
            <div class="tile-value">{{ setup.environment.docker ? `v${setup.environment.docker}` : 'Not reachable' }}</div>
            <div class="tile-sub muted small">Runs the published apps</div>
          </article>
        </div>
        <p class="setup-note muted small">
          <Icon name="info" :size="16" />
          <span>
            Rerun the installer to change how the panel is reached: <code>--tailscale</code> keeps it on the tailnet,
            <code>--allow 10.8.0.0/24</code> opens it to a VPN, <code>--proxy caddy|nginx</code> picks the reverse proxy.
          </span>
        </p>
      </template>
    </section>

    <section class="card">
      <div class="card-head">
        <h2>Devices</h2>
        <button class="btn btn-tonal st" type="button" :disabled="addingKey || !passkeysSupported()" @click="addPasskey">
          <Icon name="key" small /> Add a passkey
        </button>
      </div>
      <p class="muted small" style="margin: 0 0 8px">
        To add a phone or another browser, run <code>sudo vps-admin pair</code> on the server and enter the code there.
      </p>
      <form v-if="unnamedSelf" class="self" @submit.prevent="nameSelf">
        <Icon name="person" :size="20" />
        <div class="grow">
          <div>Who uses this browser?</div>
          <div class="muted small">Every device now has the name of its person, so admins can tell each other apart</div>
        </div>
        <label class="field self-field"><input v-model="selfOwner" placeholder="Your name" aria-label="Your name" maxlength="60" autocomplete="name" /></label>
        <button class="btn btn-filled st" type="submit" :disabled="!selfOwner.trim()">Save</button>
      </form>
      <div v-for="p in people" :key="p.owner" class="person">
        <div class="person-head">
          <span class="avatar" :class="{ none: !p.owner }">{{ p.owner ? initial(p.owner) : '?' }}</span>
          <h3>{{ p.owner || 'Not named yet' }}</h3>
          <span class="muted small">{{ p.list.length }} device{{ p.list.length === 1 ? '' : 's' }}</span>
        </div>
        <div v-for="d in p.list" :key="d.id" class="row dev">
          <span class="dev-icon"><Icon :name="d.kind === 'app' ? 'phone' : 'globe'" :size="20" /></span>
          <div class="grow">
            <div class="name">
              {{ d.name }}
              <span v-if="d.id === current" class="chip">This device</span>
            </div>
            <div class="muted small">
              {{ d.kind === 'app' ? 'Android app' : `Browser · ${d.passkeys} passkey${d.passkeys === 1 ? '' : 's'}` }} ·
              added {{ ago(d.createdAt) }} · seen {{ ago(d.lastSeenAt) }}{{ d.lastIp ? ` from ${d.lastIp}` : '' }}
            </div>
          </div>
          <button class="icon-btn st" type="button" :aria-label="`Rename ${d.name}`" v-tip="'Rename'" @click="startEdit(d)"><Icon name="edit" small /></button>
          <button class="btn btn-text st" type="button" @click="revoke(d)">Remove</button>
        </div>
      </div>
    </section>

    <M3Dialog v-model="editOpen" title="Rename the device">
      <form class="edit-form" @submit.prevent="saveEdit">
        <label class="field"><Icon name="person" :size="20" /><input v-model="editOwner" placeholder="Person" aria-label="Person using it" maxlength="60" /></label>
        <label class="field"><Icon :name="editing?.kind === 'app' ? 'phone' : 'globe'" :size="20" /><input v-model="editName" placeholder="Device" aria-label="Device name" maxlength="60" /></label>
      </form>
      <template #actions>
        <button class="btn btn-text st" type="button" @click="editOpen = false">Cancel</button>
        <button class="btn btn-filled st" type="button" :disabled="!editOwner.trim() || !editName.trim()" @click="saveEdit">Save</button>
      </template>
    </M3Dialog>

    <section class="card">
      <div class="card-head">
        <h2>Activity</h2>
        <span class="muted small">Every sign-in and action, kept for a year</span>
      </div>
      <div class="audit-list" :class="{ open: auditOpen }">
      <div v-for="e in shownAudit" :key="e.id" class="row audit">
        <span class="mark" :class="e.ok ? 'ok' : 'fail'" :aria-label="e.ok ? 'Succeeded' : 'Failed'">{{ e.ok ? '✓' : '!' }}</span>
        <div class="grow">
          <div>{{ describe(e) }}<template v-if="e.target"> · <code>{{ e.target }}</code></template></div>
          <div class="muted small">
            {{ dateTime(e.at) }} · {{ e.device || 'system' }}{{ e.ip ? ` · ${e.ip}` : '' }}<template v-if="!e.ok && e.detail"> · {{ e.detail }}</template>
          </div>
        </div>
      </div>
      </div>
      <div class="audit-actions">
        <button v-if="audit.length > 5" class="btn btn-text st" type="button" :aria-expanded="auditOpen" @click="auditOpen = !auditOpen">
          {{ auditOpen ? 'Show less' : `Show more (${audit.length - 5})` }}
          <Icon name="chevron" small :class="{ flip: auditOpen }" />
        </button>
        <button
          v-if="auditOpen && moreAudit"
          class="btn btn-tonal st"
          type="button"
          :disabled="loadingAudit"
          :aria-busy="loadingAudit"
          @click="olderAudit"
        >
          <ShapeLoader v-if="loadingAudit" small label="Loading older entries" />
          Load older
        </button>
      </div>
    </section>
  </div>
</template>

<style scoped>
/* Three cards per row: six of them fill two rows exactly, so the last one never sits
   alone beside an empty cell. */
.setup-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 12px;
}
@media (max-width: 1180px) {
  .setup-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}
@media (max-width: 720px) {
  .setup-grid {
    grid-template-columns: minmax(0, 1fr);
  }
}
.tile {
  display: flex;
  flex-direction: column;
  gap: 10px;
  min-width: 0;
  padding: 16px;
  border-radius: 20px;
  background: var(--sch);
}
.tile.skeleton {
  min-height: 128px;
  animation: tile-pulse 1.6s var(--ease) infinite;
}
@keyframes tile-pulse {
  0%,
  100% {
    opacity: 1;
  }
  50% {
    opacity: 0.5;
  }
}
.tile-head {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
}
.tile-icon {
  width: 34px;
  height: 34px;
  border-radius: 17px;
  display: grid;
  place-items: center;
  flex: none;
  background: var(--secc);
  color: var(--on-secc);
}
.tile-icon.primary {
  background: var(--pc);
  color: var(--on-pc);
}
.tile-icon.tertiary {
  background: var(--tc);
  color: var(--on-tc);
}
.tile-label {
  flex: 1;
  min-width: 0;
  font-size: 13px;
  font-weight: 600;
  letter-spacing: 0.04em;
  text-transform: uppercase;
  color: var(--on-sv);
}
.tile-value {
  font-size: 16px;
  font-weight: 500;
  overflow-wrap: anywhere;
}
.tile-value code {
  font-size: 14px;
}
/* The cards of a row are ordered so their content is about as tall: a card only gets
   a strip of spare room at the bottom instead of a hole in the middle. */
.tile-sub {
  overflow-wrap: anywhere;
}
.tile-sub code {
  font-size: 12px;
}
.tool-list {
  display: flex;
  flex-direction: column;
  gap: 10px;
  min-width: 0;
}
.tool {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
  font-size: 14px;
}
/* A long value (a tailnet name, an address) ends in an ellipsis instead of pushing the
   card wider than its column or wrapping onto a second line. */
.tool-name {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
}
.tool-value {
  flex: 0 1 auto;
  min-width: 0;
  max-width: 55%;
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
  text-align: right;
}
.tool.off {
  color: var(--on-sv);
}
.setup-note {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  margin: 14px 0 0;
}
.setup-note svg {
  flex: none;
  margin-top: 2px;
}
.setting {
  display: flex;
  align-items: center;
  gap: 16px;
  padding: 12px 0;
  border-top: 1px solid var(--sch);
  flex-wrap: wrap;
}
.setting:first-of-type {
  border-top: none;
}
.setting.col {
  flex-direction: column;
  align-items: stretch;
}
.bg-row {
  display: flex;
  flex-wrap: wrap;
  gap: 20px;
  align-items: flex-start;
}
.preview {
  width: 240px;
  aspect-ratio: 16 / 10;
  border-radius: 16px;
  overflow: hidden;
  background: var(--surface);
  border: 1px solid var(--outline-v);
  display: grid;
  place-items: center;
}
.preview img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}
.bg-controls {
  display: flex;
  flex-direction: column;
  gap: 12px;
  min-width: 240px;
}
.bg-buttons {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
.seg {
  display: inline-flex;
  border: 1px solid var(--outline);
  border-radius: 20px;
  overflow: hidden;
}
.seg button {
  height: 40px;
  padding: 0 18px;
  border: none;
  background: transparent;
  color: var(--on-surface);
  font-size: 14px;
  font-weight: 500;
}
.seg button + button {
  border-left: 1px solid var(--outline);
}
.seg button.on {
  background: var(--secc);
  color: var(--on-secc);
}
.seeds {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
}
.seed {
  width: 44px;
  height: 44px;
  border-radius: 22px;
  border: none;
  padding: 0;
  transition: box-shadow 0.2s var(--ease);
}
.seed.on {
  box-shadow:
    0 0 0 3px var(--sc),
    0 0 0 5px var(--on-surface);
}
.self {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 12px;
  margin-bottom: 12px;
  padding: 14px 16px;
  border-radius: 16px;
  background: var(--tc);
  color: var(--on-tc);
}
.self .muted {
  color: inherit;
  opacity: 0.8;
}
.self-field {
  flex: 1 1 180px;
  background: var(--surface);
}
.person + .person {
  margin-top: 8px;
  border-top: 1px solid var(--sch);
}
.person-head {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 14px 4px 6px;
}
.person-head h3 {
  margin: 0;
  font-size: 16px;
  font-weight: 500;
}
.avatar {
  width: 36px;
  height: 36px;
  border-radius: 18px;
  display: grid;
  place-items: center;
  background: var(--pc);
  color: var(--on-pc);
  font-weight: 600;
}
.avatar.none {
  background: var(--sch);
  color: var(--on-sv);
}
.dev {
  padding-left: 20px;
}
.edit-form {
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.dev-icon {
  width: 40px;
  height: 40px;
  border-radius: 20px;
  display: grid;
  place-items: center;
  background: var(--secc);
  color: var(--on-secc);
}
.name {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 16px;
  font-weight: 500;
}
.audit-list.open {
  max-height: 560px;
  overflow-y: auto;
  overscroll-behavior: contain;
}
.audit-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 8px;
}
.flip {
  rotate: 180deg;
}
.audit {
  padding: 10px 4px;
  font-size: 14px;
}
.mark {
  width: 22px;
  height: 22px;
  border-radius: 11px;
  display: grid;
  place-items: center;
  font-size: 12px;
  font-weight: 700;
}
.mark.ok {
  background: var(--ok);
  color: var(--on-ok);
}
.mark.fail {
  background: var(--bad);
  color: var(--on-bad);
}
.alert-row {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  align-items: center;
}
.alert-row .field {
  flex: 1 1 320px;
  min-width: 0;
}
.alert-disk {
  width: 120px;
  justify-content: flex-end;
}
.alert-disk input {
  width: 48px;
  text-align: right;
}
.alert-error {
  display: flex;
  gap: 8px;
  align-items: center;
  margin: 6px 0 0;
}
</style>
