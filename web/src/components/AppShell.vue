<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import Icon, { type IconName } from './Icon.vue'
import M3Shape from './M3Shape.vue'
import M3Dialog from './M3Dialog.vue'
import ServerBackground from './ServerBackground.vue'
import { loadAppearance } from '@/lib/appearance'
import { streamSystem } from '@/lib/api'
import { loadSystem, session } from '@/lib/session'
import { forget, normalizeServerURL, servers } from '@/lib/servers'
import { duration } from '@/lib/format'
import { toastError } from '@/lib/ui'

const route = useRoute()

const NAV: { to: string; label: string; icon: IconName; mobile: boolean }[] = [
  { to: '/', label: 'Overview', icon: 'overview', mobile: true },
  { to: '/containers', label: 'Containers', icon: 'containers', mobile: true },
  { to: '/services', label: 'Services', icon: 'services', mobile: true },
  { to: '/security', label: 'Security', icon: 'security', mobile: false },
  { to: '/apps', label: 'Apps', icon: 'apps', mobile: true },
  { to: '/assistant', label: 'Assistant', icon: 'assistant', mobile: true },
  { to: '/scripts', label: 'Scripts', icon: 'terminal', mobile: false },
  { to: '/updates', label: 'Updates', icon: 'updates', mobile: false },
  { to: '/settings', label: 'Settings', icon: 'settings', mobile: true },
]
const isActive = (to: string) => (to === '/' ? route.path === '/' : route.path.startsWith(to))

const menuOpen = ref(false)
const addOpen = ref(false)
const newURL = ref('')
const here = location.origin
const others = computed(() => servers.value.filter((s) => s.url !== here))

function addServer() {
  const url = normalizeServerURL(newURL.value)
  if (!url) return toastError('Enter the address of the other panel, like admin.example.com')
  location.href = url
}

function closeMenu(e: MouseEvent) {
  if (!(e.target as HTMLElement).closest('.switcher')) menuOpen.value = false
}

let stop: (() => void) | undefined
onMounted(async () => {
  document.addEventListener('click', closeMenu)
  try {
    loadAppearance().catch(() => {})
    await loadSystem()
    stop = streamSystem((s) => (session.snapshot = s))
  } catch (e) {
    toastError(e)
  }
})
onBeforeUnmount(() => {
  document.removeEventListener('click', closeMenu)
  stop?.()
})
</script>

<template>
  <div class="shell">
    <ServerBackground />
    <nav class="rail" aria-label="Sections">
      <RouterLink to="/" class="logo" aria-label="VPS Admin home">
        <M3Shape kind="cookie" :size="48" fill="var(--pc)">
          <path d="M32 40h36M32 50h36M32 60h24" stroke="var(--on-pc)" stroke-width="6" stroke-linecap="round" />
        </M3Shape>
      </RouterLink>
      <RouterLink
        v-for="n in NAV"
        :key="n.to"
        :to="n.to"
        class="nav-item"
        :class="{ active: isActive(n.to), desktop: !n.mobile }"
        :aria-current="isActive(n.to) ? 'page' : undefined"
      >
        <span class="pill st"><Icon :name="n.icon" /></span>
        {{ n.label }}
      </RouterLink>
    </nav>

    <div class="main-col">
      <header class="topbar">
        <span class="brand">VPS ADMIN</span>
        <div class="switcher">
          <button
            class="server st"
            type="button"
            aria-haspopup="menu"
            :aria-expanded="menuOpen"
            @click.stop="menuOpen = !menuOpen"
          >
            <span class="online" aria-hidden="true" />
            <span class="ellipsis">{{ session.host?.hostname ?? 'Connecting…' }}</span>
            <Icon name="chevron" :size="18" />
          </button>
          <div v-if="menuOpen" class="menu" role="menu">
            <div class="menu-item current" role="menuitem" aria-current="true">
              <Icon name="check" :size="18" />
              <span class="grow ellipsis">{{ session.host?.hostname }}<br /><small class="muted">{{ here }}</small></span>
            </div>
            <div v-for="s in others" :key="s.url" class="menu-row">
              <a class="menu-item st" role="menuitem" :href="s.url">
                <Icon name="globe" :size="18" />
                <span class="grow ellipsis">{{ s.name }}<br /><small class="muted">{{ s.url }}</small></span>
              </a>
              <button class="icon-btn st" type="button" :aria-label="`Forget ${s.name}`" @click="forget(s.url)">
                <Icon name="close" :size="18" />
              </button>
            </div>
            <button class="menu-item st" role="menuitem" type="button" @click="(addOpen = true), (menuOpen = false)">
              <Icon name="plus" :size="18" /> Add a server
            </button>
          </div>
        </div>
        <span v-if="session.host && session.snapshot" class="chip os">
          {{ session.host.os }} · up {{ duration(session.snapshot.uptime) }}
        </span>
        <span class="spacer" />
      </header>

      <main class="content">
        <slot />
      </main>
    </div>

    <M3Dialog v-model="addOpen" title="Add a server">
      <p class="muted" style="margin: 0">
        Install VPS Admin on the other server, then open its panel here. Each server has its own
        address and its own passkey.
      </p>
      <code class="install">curl -fsSL https://github.com/beedywool/vps-admin/releases/latest/download/install.sh | sudo bash -s -- --domain admin.example.com</code>
      <form @submit.prevent="addServer">
        <label class="field">
          <Icon name="globe" :size="20" />
          <input v-model="newURL" placeholder="admin.example.com" aria-label="Panel address" autocomplete="url" />
        </label>
      </form>
      <template #actions>
        <button class="btn btn-text st" type="button" @click="addOpen = false">Cancel</button>
        <button class="btn btn-filled st" type="button" @click="addServer">Open panel</button>
      </template>
    </M3Dialog>
  </div>
</template>

<style scoped>
.shell {
  display: flex;
  min-height: 100vh;
}
.rail {
  position: sticky;
  top: 0;
  height: 100vh;
  flex: 0 0 96px;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 6px;
  padding: 20px 0;
  border-right: 1px solid var(--sch);
}
.logo {
  margin-bottom: 18px;
  display: grid;
}
.nav-item {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 4px;
  width: 80px;
  padding: 4px 0;
  color: var(--on-sv);
  font-size: 12px;
  font-weight: 500;
  text-decoration: none;
}
.pill {
  width: 56px;
  height: 32px;
  border-radius: 16px;
  display: grid;
  place-items: center;
  transition: background 0.2s var(--ease), width 0.3s var(--ease-decel);
}
.nav-item.active {
  color: var(--on-surface);
}
.nav-item.active .pill {
  background: var(--secc);
  color: var(--on-secc);
}
.main-col {
  flex: 1;
  min-width: 0;
  padding: 24px 32px 48px;
}
.content {
  max-width: 1340px;
}
.topbar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 12px;
  margin-bottom: 20px;
}
.brand {
  font-size: 30px;
  font-weight: 500;
  letter-spacing: 0.14em;
  margin-right: 8px;
}
.switcher {
  position: relative;
}
.server {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  max-width: 320px;
  height: 40px;
  padding: 0 12px 0 14px;
  border-radius: 20px;
  border: 1px solid var(--outline-v);
  background: transparent;
  color: var(--on-surface);
  font-size: 14px;
}
.online {
  width: 8px;
  height: 8px;
  border-radius: 4px;
  background: var(--ok);
  flex-shrink: 0;
}
.menu {
  position: absolute;
  top: 46px;
  left: 0;
  z-index: 20;
  min-width: 300px;
  padding: 8px 0;
  border-radius: 16px;
  background: var(--schh);
  box-shadow: 0 8px 24px rgba(0, 0, 0, 0.35);
}
.menu-row {
  display: flex;
  align-items: center;
  padding-right: 6px;
}
.menu-item {
  display: flex;
  align-items: center;
  gap: 12px;
  width: 100%;
  min-height: 48px;
  padding: 8px 16px;
  border: none;
  background: transparent;
  color: var(--on-surface);
  font-size: 14px;
  text-align: left;
  text-decoration: none;
}
.menu-item.current {
  color: var(--primary);
}
.os {
  font-size: 13px;
}
.spacer {
  flex: 1;
}
.install {
  display: block;
  padding: 12px;
  border-radius: 12px;
  background: var(--scc);
  border: 1px solid var(--outline-v);
  color: var(--primary);
  font-size: 12px;
  line-height: 1.5;
  word-break: break-all;
}

/* Phones: the rail becomes a bottom navigation bar */
@media (max-width: 760px) {
  .rail {
    position: fixed;
    top: auto;
    bottom: 0;
    left: 0;
    right: 0;
    z-index: 10;
    height: 80px;
    flex-direction: row;
    justify-content: space-around;
    gap: 0;
    padding: 12px 0 0;
    border-right: none;
    background: var(--sc);
  }
  .logo,
  .nav-item.desktop {
    display: none;
  }
  .nav-item {
    width: auto;
    flex: 1;
    min-width: 0;
    font-size: 11px;
  }
  .pill {
    width: 48px;
  }
  .main-col {
    padding: 16px 16px 112px;
  }
  .brand {
    display: none;
  }
  .os {
    display: none;
  }
}
</style>
