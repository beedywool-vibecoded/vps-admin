<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import Icon from '@/components/Icon.vue'
import M3Slider from '@/components/M3Slider.vue'
import { appearance, loadAppearance, removeBackground, setOpacity, uploadBackground } from '@/lib/appearance'
import { api, type AuditEntry, type Device } from '@/lib/api'
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
const addingKey = ref(false)
const fileInput = ref<HTMLInputElement>()
const uploading = ref(false)
const opacity = ref(appearance.opacity)

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
const sortedDevices = computed(() => [...devices.value].sort((a, b) => (a.id === current.value ? -1 : b.id === current.value ? 1 : 0)))

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
    title: `Remove ${d.name}?`,
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
        <h2>Devices</h2>
        <button class="btn btn-tonal st" type="button" :disabled="addingKey || !passkeysSupported()" @click="addPasskey">
          <Icon name="key" :size="18" /> Add a passkey
        </button>
      </div>
      <p class="muted small" style="margin: 0 0 8px">
        To add a phone or another browser, run <code>sudo vps-admin pair</code> on the server and enter the code there.
      </p>
      <div v-for="d in sortedDevices" :key="d.id" class="row">
        <span class="dev-icon"><Icon :name="d.kind === 'app' ? 'phone' : 'globe'" :size="20" /></span>
        <div class="grow">
          <div class="name">
            {{ d.name }}
            <span v-if="d.id === current" class="chip">This device</span>
          </div>
          <div class="muted small">
            {{ d.kind === 'app' ? 'App' : `Browser · ${d.passkeys} passkey${d.passkeys === 1 ? '' : 's'}` }} ·
            added {{ ago(d.createdAt) }} · seen {{ ago(d.lastSeenAt) }}{{ d.lastIp ? ` from ${d.lastIp}` : '' }}
          </div>
        </div>
        <button class="btn btn-text st" type="button" @click="revoke(d)">Remove</button>
      </div>
    </section>

    <section class="card">
      <div class="card-head">
        <h2>Activity</h2>
        <span class="muted small">Every sign-in and action, kept for a year</span>
      </div>
      <div v-for="e in audit" :key="e.id" class="row audit">
        <span class="mark" :class="e.ok ? 'ok' : 'fail'" :aria-label="e.ok ? 'Succeeded' : 'Failed'">{{ e.ok ? '✓' : '!' }}</span>
        <div class="grow">
          <div>{{ describe(e) }}<template v-if="e.target"> · <code>{{ e.target }}</code></template></div>
          <div class="muted small">
            {{ dateTime(e.at) }} · {{ e.device || 'system' }}{{ e.ip ? ` · ${e.ip}` : '' }}<template v-if="!e.ok && e.detail"> · {{ e.detail }}</template>
          </div>
        </div>
      </div>
      <button v-if="moreAudit && audit.length" class="btn btn-text st" type="button" @click="loadAudit(true)">Show older</button>
    </section>
  </div>
</template>

<style scoped>
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
