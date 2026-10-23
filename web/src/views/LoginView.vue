<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import M3Shape from '@/components/M3Shape.vue'
import ShapeLoader from '@/components/ShapeLoader.vue'
import Icon from '@/components/Icon.vue'
import { api, ApiError, type AuthState } from '@/lib/api'
import { loginWithPasskey, passkeysSupported, registerPasskey } from '@/lib/passkey'
import { resetAuthCheck } from '@/router'

const route = useRoute()
const router = useRouter()

const state = ref<AuthState | null>(null)
const step = ref<'loading' | 'signin' | 'pair' | 'enroll'>('loading')
const code = ref('')
const deviceName = ref(defaultName())
const owner = ref('')
const busy = ref(false)
const error = ref('')

function defaultName(): string {
  const ua = navigator.userAgent
  const browser = /Firefox\//.test(ua) ? 'Firefox' : /Edg\//.test(ua) ? 'Edge' : /Chrome\//.test(ua) ? 'Chrome' : /Safari\//.test(ua) ? 'Safari' : 'Browser'
  const os = /Android/.test(ua) ? 'Android' : /iPhone|iPad/.test(ua) ? 'iOS' : /Mac OS/.test(ua) ? 'macOS' : /Windows/.test(ua) ? 'Windows' : /Linux/.test(ua) ? 'Linux' : ''
  return os ? `${browser} on ${os}` : browser
}

// Chrome on Android tells the phone model through client hints: "Chrome on Pixel 10 Pro Fold".
type UAData = { getHighEntropyValues(h: string[]): Promise<{ model?: string }> }
async function refineName() {
  const data = (navigator as Navigator & { userAgentData?: UAData }).userAgentData
  if (!data) return
  try {
    const { model } = await data.getHighEntropyValues(['model'])
    if (model && deviceName.value === defaultName()) deviceName.value = `${deviceName.value.split(' on ')[0]} on ${model}`
  } catch {
    // no hints: the generic name stays
  }
}

const canPasskey = computed(() => passkeysSupported() && (state.value?.secureOrigin ?? false))

async function refresh() {
  state.value = await api.authState()
  if (state.value.scope === 'full') return done()
  if (state.value.scope === 'enroll') step.value = 'enroll'
  else step.value = state.value.hasPasskeys ? 'signin' : 'pair'
}

function done() {
  resetAuthCheck()
  const next = typeof route.query.next === 'string' && route.query.next.startsWith('/') ? route.query.next : '/'
  router.replace(next)
}

// Formats as XXX-XXX-XXX while typing, keeping only the characters codes use.
function onCodeInput(e: Event) {
  const raw = (e.target as HTMLInputElement).value.toUpperCase().replace(/[^23456789ABCDEFGHJKMNPQRSTUVWXYZ]/g, '').slice(0, 9)
  code.value = raw.replace(/(.{3})(?=.)/g, '$1-')
}

function message(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.code === 'bad_code') return 'This code is wrong, expired or already used. Run `sudo vps-admin pair` for a new one.'
    if (e.code === 'rate_limited') return 'Too many attempts. Wait a few minutes and try again.'
    if (e.code === 'passkey_rejected') return 'The passkey was not accepted. Try again, or pair this browser with a code.'
    return e.message
  }
  if (e instanceof DOMException && (e.name === 'NotAllowedError' || e.name === 'AbortError')) return 'Cancelled.'
  return e instanceof Error ? e.message : String(e)
}

async function attempt(fn: () => Promise<void>) {
  busy.value = true
  error.value = ''
  try {
    await fn()
  } catch (e) {
    error.value = message(e)
  } finally {
    busy.value = false
  }
}

const pair = () =>
  attempt(async () => {
    await api.pair(code.value, deviceName.value, owner.value.trim())
    await refresh()
  })
const enroll = () =>
  attempt(async () => {
    await registerPasskey()
    done()
  })
const signin = () =>
  attempt(async () => {
    await loginWithPasskey()
    done()
  })

onMounted(() => {
  refineName()
  attempt(refresh)
})
</script>

<template>
  <main class="login">
    <section class="panel" aria-live="polite">
      <M3Shape kind="cookie" :size="72" fill="var(--pc)" class="spin">
        <path d="M32 40h36M32 50h36M32 60h24" stroke="var(--on-pc)" stroke-width="6" stroke-linecap="round" />
      </M3Shape>
      <p class="brand">VPS ADMIN</p>

      <div v-if="state && !state.secureOrigin" class="warn" role="alert">
        <Icon name="alert" />
        <span>
          This panel is not served over https, so passkeys cannot work. Put it behind a reverse proxy with
          TLS and set <code>public_url</code> in <code>/etc/vps-admin/config.json</code>.
        </span>
      </div>

      <template v-if="step === 'loading'">
        <p class="muted connect"><ShapeLoader :size="24" label="Connecting" /> Connecting…</p>
      </template>

      <template v-else-if="step === 'signin'">
        <h1>Welcome back</h1>
        <p class="muted">Sign in with the passkey you created on this server.</p>
        <button class="btn btn-filled btn-lg st wide" type="button" :disabled="busy || !canPasskey" @click="signin">
          <Icon name="key" :size="20" /> Sign in with a passkey
        </button>
        <button class="btn btn-text st" type="button" @click="(step = 'pair'), (error = '')">Pair this browser with a code</button>
      </template>

      <template v-else-if="step === 'pair'">
        <h1>Pair this browser</h1>
        <p class="muted">
          On the server, run <code>sudo vps-admin pair</code> and type the code it shows. It works once and
          expires after 10 minutes.
        </p>
        <form class="form" @submit.prevent="pair">
          <label class="sr-only" for="code">Pairing code</label>
          <input
            id="code"
            class="code"
            :value="code"
            inputmode="text"
            autocomplete="one-time-code"
            autocapitalize="characters"
            spellcheck="false"
            placeholder="XXX-XXX-XXX"
            @input="onCodeInput"
          />
          <label class="field">
            <Icon name="person" :size="20" />
            <input v-model="owner" placeholder="Your name" aria-label="Your name" maxlength="60" autocomplete="name" required />
          </label>
          <label class="field">
            <Icon name="globe" :size="20" />
            <input v-model="deviceName" aria-label="Name of this device" maxlength="60" />
          </label>
          <p class="muted small hint">Your name tells the other admins who uses this device, in the device list and the activity log.</p>
          <button class="btn btn-filled btn-lg st wide" type="submit" :disabled="busy || code.length !== 11 || !owner.trim() || !deviceName.trim()">Pair</button>
        </form>
        <button v-if="state?.hasPasskeys" class="btn btn-text st" type="button" @click="(step = 'signin'), (error = '')">
          I already have a passkey
        </button>
      </template>

      <template v-else-if="step === 'enroll'">
        <h1>Create your passkey</h1>
        <p class="muted">
          Paired. Now save a passkey (fingerprint, face, PIN or security key): it is what lets you sign in
          from now on. Codes are only for new devices.
        </p>
        <button class="btn btn-filled btn-lg st wide" type="button" :disabled="busy || !canPasskey" @click="enroll">
          <Icon name="key" :size="20" /> Create a passkey
        </button>
      </template>

      <p v-if="error" class="error" role="alert">
        <template v-for="(part, i) in error.split(/`([^`]+)`/)" :key="i"><code v-if="i % 2">{{ part }}</code><template v-else>{{ part }}</template></template>
      </p>
    </section>
  </main>
</template>

<style scoped>
.hint {
  margin: -4px 4px 0;
  text-align: left;
}
.login {
  min-height: 100vh;
  display: grid;
  place-items: center;
  padding: 24px 16px;
}
.panel {
  width: min(460px, 100%);
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 14px;
  padding: 36px 32px;
  border-radius: 32px;
  background: var(--sc);
  text-align: center;
}
.connect {
  display: flex;
  align-items: center;
  gap: 10px;
}
.brand {
  margin: 0 0 6px;
  font-size: 15px;
  font-weight: 600;
  letter-spacing: 0.18em;
  color: var(--on-sv);
}
h1 {
  font-size: 28px;
}
p {
  margin: 0;
  line-height: 1.55;
}
.form {
  display: flex;
  flex-direction: column;
  gap: 12px;
  width: 100%;
}
.code {
  width: 100%;
  height: 64px;
  border-radius: 16px;
  border: 2px solid var(--outline);
  background: transparent;
  text-align: center;
  font-family: var(--mono);
  font-size: 28px;
  letter-spacing: 0.12em;
  color: var(--on-surface);
  outline: none;
}
.code:focus {
  border-color: var(--primary);
}
.wide {
  width: 100%;
}
.warn {
  display: flex;
  gap: 12px;
  padding: 14px 16px;
  border-radius: 16px;
  background: var(--ec);
  color: var(--on-ec);
  font-size: 14px;
  text-align: left;
}
.error {
  color: var(--error);
  font-size: 14px;
}
.spin {
  animation: spin 40s linear infinite;
}
@keyframes spin {
  to {
    rotate: 360deg;
  }
}
</style>
