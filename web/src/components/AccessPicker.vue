<script setup lang="ts">
// How an app is reached, limited to what the server has: public domain via the proxy,
// VPN-only, Tailscale, or 127.0.0.1.
import { computed, onMounted, ref } from 'vue'
import Icon from './Icon.vue'
import { api, type AccessMode, type ServerSetup } from '@/lib/api'

const access = defineModel<AccessMode>('access', { required: true })
const domain = defineModel<string>('domain', { required: true })
const allow = defineModel<string>('allow', { required: true })
defineProps<{ tsPort?: number }>()

const setup = ref<ServerSetup | null>(null)
onMounted(async () => {
  try {
    setup.value = await api.environment()
  } catch {
    // offer every mode; the server still checks
  }
})

const env = computed(() => setup.value?.environment)
const proxy = computed(() => {
  const e = env.value
  if (!e) return ''
  if (e.caddy.installed && setup.value?.sites) return 'Caddy, with automatic HTTPS'
  if (e.nginx.installed && setup.value?.sites) return e.certbot.installed ? 'nginx, HTTPS by certbot' : 'nginx (install certbot for HTTPS)'
  return ''
})
// Networks of the VPN links found here, offered in one tap.
const vpnNets = computed(() => {
  const e = env.value
  if (!e) return []
  const out: { label: string; cidr: string }[] = []
  for (const l of [...e.wireguard, ...e.zerotier]) for (const a of l.addresses) if (a.includes('.')) out.push({ label: `${l.name} ${a}`, cidr: a })
  if (e.tailscale?.running) out.push({ label: 'Tailscale 100.64.0.0/10', cidr: '100.64.0.0/10' })
  return out
})
function addNet(cidr: string) {
  const list = allow.value.split(/[\s,]+/).filter(Boolean)
  if (!list.includes(cidr)) allow.value = [...list, cidr].join(', ')
}

const options = computed(() => {
  const e = env.value
  const ts = e?.tailscale
  return [
    {
      id: 'public' as const,
      icon: 'globe' as const,
      title: 'Public domain',
      sub: proxy.value || (setup.value ? 'No reverse proxy set up by the installer' : 'Through the reverse proxy'),
      disabled: !!setup.value && !setup.value.sites,
    },
    {
      id: 'vpn' as const,
      icon: 'lock' as const,
      title: 'VPN only',
      sub: 'The domain answers only to the networks you list (WireGuard, ZeroTier, an office)',
      disabled: !!setup.value && !setup.value.sites,
    },
    {
      id: 'tailscale' as const,
      icon: 'security' as const,
      title: 'Tailscale only',
      sub: !e
        ? 'On this server\'s tailnet name'
        : !ts
          ? 'Tailscale is not installed'
          : !ts.running
            ? 'Tailscale is not running'
            : !ts.https
              ? 'Enable HTTPS in the Tailscale console (DNS page) first'
              : `https://${ts.name}:port, private to your tailnet`,
      disabled: !!e && !(ts?.running && ts.https),
    },
    { id: 'local' as const, icon: 'server' as const, title: 'No domain', sub: 'Reachable on 127.0.0.1 only, for other containers or an SSH tunnel', disabled: false },
  ]
})
</script>

<template>
  <div class="access">
    <div class="opts" role="radiogroup" aria-label="Access">
      <button
        v-for="o in options"
        :key="o.id"
        type="button"
        role="radio"
        class="opt st"
        :class="{ on: access === o.id }"
        :aria-checked="access === o.id"
        :disabled="o.disabled && access !== o.id"
        @click="access = o.id"
      >
        <Icon :name="o.icon" :size="20" />
        <span class="txt">
          <span class="t">{{ o.title }}</span>
          <span class="s">{{ o.sub }}</span>
        </span>
      </button>
    </div>
    <div v-if="access === 'public' || access === 'vpn'" class="grid">
      <div class="ffield">
        <label class="tf">
          <span>Domain</span>
          <input v-model.trim="domain" placeholder="app.example.com" spellcheck="false" />
        </label>
        <small>Point its DNS to this server; HTTPS is automatic</small>
      </div>
      <div v-if="access === 'vpn'" class="ffield">
        <label class="tf">
          <span>Allowed networks</span>
          <input v-model.trim="allow" placeholder="10.8.0.0/24, 192.168.1.0/24" spellcheck="false" />
        </label>
        <small>Your devices must reach the domain through these networks</small>
      </div>
    </div>
    <div v-if="access === 'vpn' && vpnNets.length" class="nets">
      <span class="muted small">Found here:</span>
      <button v-for="n in vpnNets" :key="n.cidr" type="button" class="chip chip-outline st net" @click="addNet(n.cidr)">
        <Icon name="plus" :size="14" /> {{ n.label }}
      </button>
    </div>
    <p v-if="access === 'tailscale' && tsPort && env?.tailscale" class="muted small" style="margin: 0">
      Served at <code>https://{{ env.tailscale.name }}:{{ tsPort }}</code>
    </p>
  </div>
</template>

<style scoped>
.access {
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.opts {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(min(240px, 100%), 1fr));
  gap: 8px;
}
.opt {
  display: flex;
  align-items: flex-start;
  gap: 12px;
  padding: 14px 16px;
  border: 1px solid var(--outline-v);
  border-radius: 16px;
  background: transparent;
  color: var(--on-surface);
  text-align: left;
  font: inherit;
}
.opt.on {
  background: var(--secc);
  border-color: var(--secc);
  color: var(--on-secc);
}
.txt {
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.t {
  font-weight: 500;
}
.s {
  font-size: 12px;
  opacity: 0.8;
}
.grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(min(260px, 100%), 1fr));
  align-items: start;
  gap: 12px;
}
/* Hint under the box, so fields in a row are the same height. */
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
  overflow: hidden;
  font-size: 12px;
  line-height: 16px;
  font-weight: 500;
  color: var(--primary);
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
.nets {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
}
.net {
  border: 1px solid var(--outline-v);
  background: transparent;
  font: inherit;
  font-size: 12px;
}
</style>
