<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import Icon from '@/components/Icon.vue'
import M3Slider from '@/components/M3Slider.vue'
import M3Dialog from '@/components/M3Dialog.vue'
import { appearance, loadAppearance, removeBackground, setOpacity, uploadBackground } from '@/lib/appearance'
import { api, type AuditEntry, type Device, type ServerSetup } from '@/lib/api'
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
const setup = ref<ServerSetup | null>(null)
const proxyTools = computed(() =>
  setup.value
    ? [
        { name: 'Caddy', tool: setup.value.environment.caddy },
        { name: 'nginx', tool: setup.value.environment.nginx },
        { name: setup.value.environment.certbot.nginxPlugin ? 'certbot (nginx plugin)' : 'certbot', tool: setup.value.environment.certbot },
      ]
    : [],
)

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
}
const describe = (e: AuditEntry) => {
  if (ACTIONS[e.action]) return ACTIONS[e.action]
  const [kind, verb] = e.action.split('.')
  return `${verb ? verb.charAt(0).toUpperCase() + verb.slice(1) : e.action} ${kind === 'container' ? 'container' : kind === 'service' ? 'service' : ''}`.trim()
}

onMounted(async () => {
  try {
    api.environment().then((e) => (setup.value = e)).catch(() => {})
    await Promise.all([loadDevices(), loadAudit(), loadAppearance()])
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
      <button class="btn btn-outlined st" type="button" @click="logout"><Icon name="logout" :size="18" /> Sign out</button>
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
              <Icon name="plus" :size="18" /> {{ appearance.background ? 'Change image' : 'Choose an image' }}
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
        <h2>Server setup</h2>
        <span class="muted small">What this server offers to publish apps, detected {{ setup ? ago(setup.environment.checked) : '…' }}</span>
      </div>
      <p v-if="!setup" class="muted small">Looking at the server…</p>
      <template v-else>
        <div class="env">
          <div class="env-row">
            <span class="env-k">Panel</span>
            <span class="grow">
              <code class="plain">{{ setup.panel.url }}</code>
              <span class="chip" :class="setup.panel.access === 'public' ? 'chip-outline' : ''">{{ setup.panel.access === 'tailscale' ? 'Tailscale only' : setup.panel.access === 'local' ? 'Local' : 'Public' }}</span>
              <span v-if="setup.panel.webhookUrl" class="muted small"> · GitHub webhooks via {{ setup.panel.webhookUrl }}</span>
            </span>
          </div>
          <div class="env-row">
            <span class="env-k">Reverse proxy</span>
            <span class="grow tools">
              <span v-for="t in proxyTools" :key="t.name" class="tool" :class="{ off: !t.tool.installed }">
                <StatusDot :tone="!t.tool.installed ? 'idle' : t.tool.running ? 'ok' : 'warn'" :label="t.tool.installed ? (t.tool.running ? 'running' : 'stopped') : 'not installed'" />
                {{ t.name }}<span v-if="t.tool.version" class="muted small"> {{ t.tool.version }}</span>
              </span>
              <span v-if="setup.environment.traefik" class="tool"><StatusDot tone="ok" label="running" /> Traefik <span class="muted small">(in Docker, managed outside the panel)</span></span>
            </span>
          </div>
          <div class="env-row">
            <span class="env-k">App domains</span>
            <span class="grow">{{ setup.sites ? (setup.environment.caddy.installed && setup.environment.caddy.running ? 'Through Caddy, HTTPS by Caddy' : 'Through nginx, HTTPS by certbot') : 'Not set up: rerun the installer with --domain' }}</span>
          </div>
          <div class="env-row">
            <span class="env-k">Private networks</span>
            <span class="grow tools">
              <span class="tool" :class="{ off: !setup.environment.tailscale }">
                <StatusDot :tone="!setup.environment.tailscale ? 'idle' : setup.environment.tailscale.running ? 'ok' : 'warn'" :label="setup.environment.tailscale ? (setup.environment.tailscale.running ? 'running' : 'stopped') : 'not installed'" />
                Tailscale<template v-if="setup.environment.tailscale?.running"><span class="muted small"> {{ setup.environment.tailscale.name }} · HTTPS {{ setup.environment.tailscale.https ? 'on' : 'off' }}</span></template>
              </span>
              <span v-for="l in setup.environment.wireguard" :key="l.name" class="tool"><StatusDot tone="ok" label="up" /> WireGuard {{ l.name }} <span class="muted small">{{ l.addresses.join(', ') }}</span></span>
              <span v-for="l in setup.environment.zerotier" :key="l.name" class="tool"><StatusDot tone="ok" label="up" /> ZeroTier {{ l.name }} <span class="muted small">{{ l.addresses.join(', ') }}</span></span>
              <span v-if="!setup.environment.wireguard.length && !setup.environment.zerotier.length" class="tool off"><StatusDot tone="idle" label="none" /> WireGuard, ZeroTier</span>
            </span>
          </div>
          <div class="env-row">
            <span class="env-k">Firewall</span>
            <span class="grow">ufw {{ setup.environment.ufw || 'not installed' }} · nftables {{ setup.environment.nftables ? 'active' : 'not active' }}</span>
          </div>
          <div class="env-row">
            <span class="env-k">Docker</span>
            <span class="grow">{{ setup.environment.docker || 'not reachable' }}</span>
          </div>
        </div>
        <p class="muted small" style="margin: 8px 0 0">
          Change how the panel is reached by rerunning the installer: <code>--tailscale</code> (private), <code>--allow 10.8.0.0/24</code> (VPN networks), <code>--proxy caddy|nginx</code>.
        </p>
      </template>
    </section>

    <section class="card">
      <div class="card-head">
        <h2>Devices</h2>
        <button class="btn btn-tonal st" type="button" :disabled="addingKey || !passkeysSupported()" @click="addPasskey">
          <Icon name="key" :size="18" /> Add a passkey
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
          <button class="icon-btn st" type="button" :aria-label="`Rename ${d.name}`" v-tip="'Rename'" @click="startEdit(d)"><Icon name="edit" :size="18" /></button>
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
          <Icon name="chevron" :size="18" :class="{ flip: auditOpen }" />
        </button>
        <button v-if="auditOpen && moreAudit" class="btn btn-tonal st" type="button" :disabled="loadingAudit" @click="olderAudit">
          {{ loadingAudit ? 'Loading…' : 'Load older' }}
        </button>
      </div>
    </section>
  </div>
</template>

<style scoped>
.env {
  display: flex;
  flex-direction: column;
}
.env-row {
  display: flex;
  flex-wrap: wrap;
  gap: 4px 16px;
  padding: 10px 0;
  border-top: 1px solid var(--sch);
  font-size: 14px;
}
.env-row:first-child {
  border-top: none;
}
.env-k {
  width: 140px;
  color: var(--on-sv);
}
.tools {
  display: flex;
  flex-wrap: wrap;
  gap: 8px 18px;
}
.tool {
  display: inline-flex;
  align-items: center;
  gap: 8px;
}
.tool.off {
  color: var(--on-sv);
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
</style>
