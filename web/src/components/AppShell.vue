<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import Icon, { type IconName } from './Icon.vue'
import M3Shape from './M3Shape.vue'
import ServerBackground from './ServerBackground.vue'
import WidgetBar from './WidgetBar.vue'
import { barOpen, barShown, wide } from '@/lib/widgets'
import { loadAppearance } from '@/lib/appearance'
import { streamSystem } from '@/lib/api'
import { loadSystem, session } from '@/lib/session'
import { lockScroll, unlockScroll } from '@/lib/scrollLock'
import { duration } from '@/lib/format'
import { toastError } from '@/lib/ui'

const route = useRoute()

// On phones the bar keeps the first four, the others go in the More sheet.
const NAV: { to: string; label: string; icon: IconName; mobile: boolean }[] = [
  { to: '/', label: 'Overview', icon: 'overview', mobile: true },
  { to: '/containers', label: 'Containers', icon: 'containers', mobile: true },
  { to: '/services', label: 'Services', icon: 'services', mobile: false },
  { to: '/proxy', label: 'Proxy', icon: 'globe', mobile: false },
  { to: '/security', label: 'Security', icon: 'security', mobile: false },
  { to: '/apps', label: 'Apps', icon: 'apps', mobile: true },
  { to: '/assistant', label: 'Assistant', icon: 'assistant', mobile: true },
  { to: '/scripts', label: 'Scripts', icon: 'terminal', mobile: false },
  { to: '/updates', label: 'Updates', icon: 'updates', mobile: false },
  { to: '/backups', label: 'Backups', icon: 'cloud', mobile: false },
  { to: '/settings', label: 'Settings', icon: 'settings', mobile: false },
]
const MORE = NAV.filter((n) => !n.mobile)
const isActive = (to: string) => (to === '/' ? route.path === '/' : route.path.startsWith(to))
const moreActive = computed(() => MORE.some((n) => isActive(n.to)))

const moreOpen = ref(false)
watch(moreOpen, (v) => (v ? lockScroll() : unlockScroll()))
watch(() => route.path, () => (moreOpen.value = false))
function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape') moreOpen.value = false
}

let stop: (() => void) | undefined
onMounted(async () => {
  document.addEventListener('keydown', onKey)
  try {
    loadAppearance().catch(() => {})
    await loadSystem()
    stop = streamSystem((s) => (session.snapshot = s))
  } catch (e) {
    toastError(e)
  }
})
onBeforeUnmount(() => {
  document.removeEventListener('keydown', onKey)
  if (moreOpen.value) unlockScroll()
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
      <button type="button" class="nav-item more-btn" :class="{ active: moreActive || moreOpen }" :aria-expanded="moreOpen" @click="moreOpen = !moreOpen">
        <span class="pill st"><Icon name="more" /></span>
        More
      </button>
    </nav>

    <Transition name="sheet">
      <div v-if="moreOpen" class="scrim" @click.self="moreOpen = false">
        <nav class="sheet" aria-label="More sections">
          <span class="handle" aria-hidden="true" />
          <RouterLink v-for="n in MORE" :key="n.to" :to="n.to" class="sheet-item st" :class="{ active: isActive(n.to) }" :aria-current="isActive(n.to) ? 'page' : undefined">
            <span class="pill"><Icon :name="n.icon" /></span>
            {{ n.label }}
          </RouterLink>
        </nav>
      </div>
    </Transition>

    <div class="main-col">
      <header class="topbar">
        <span class="brand">VPS ADMIN</span>
        <span class="server">
          <span class="online" aria-hidden="true" />
          <span class="ellipsis">{{ session.host?.hostname ?? 'Connecting…' }}</span>
        </span>
        <span v-if="session.host && session.snapshot" class="chip os">
          {{ session.host.os }} · up {{ duration(session.snapshot.uptime) }}
        </span>
        <span class="spacer" />
        <button v-if="wide && !barOpen" class="btn btn-tonal st" type="button" @click="barOpen = true">
          <Icon name="chart" :size="18" /> Widgets
        </button>
      </header>

      <main class="content">
        <slot />
      </main>
    </div>
    <WidgetBar v-if="barShown" />

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
  /* The sections are taller than a short window: the rail scrolls, the page does not. */
  overflow-y: auto;
  overscroll-behavior: contain;
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
  overflow-x: clip;
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
.server {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  max-width: 320px;
  height: 40px;
  padding: 0 16px 0 14px;
  border-radius: 20px;
  border: 1px solid var(--outline-v);
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
.os {
  font-size: 13px;
}
.more-btn,
.scrim {
  display: none;
}
.more-btn {
  border: none;
  background: none;
  font-family: inherit;
}
.spacer {
  flex: 1;
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
  .more-btn {
    display: flex;
  }
  .scrim {
    display: block;
    position: fixed;
    inset: 0;
    z-index: 30;
    background: rgba(0, 0, 0, 0.4);
  }
  .sheet {
    position: absolute;
    left: 0;
    right: 0;
    bottom: 0;
    display: flex;
    flex-direction: column;
    padding: 10px 12px 24px;
    border-radius: 28px 28px 0 0;
    background: var(--scl, var(--sc));
    transition: translate 0.3s var(--ease-decel);
  }
  .handle {
    align-self: center;
    width: 32px;
    height: 4px;
    margin-bottom: 14px;
    border-radius: 2px;
    background: var(--on-sv);
    opacity: 0.5;
  }
  .sheet-item {
    display: flex;
    align-items: center;
    gap: 16px;
    min-height: 56px;
    padding: 0 12px;
    border-radius: 28px;
    color: var(--on-surface);
    font-size: 16px;
    font-weight: 500;
    text-decoration: none;
  }
  .sheet-item.active {
    background: var(--secc);
    color: var(--on-secc);
  }
  .sheet-item .pill {
    width: 40px;
  }
  .sheet-enter-from,
  .sheet-leave-to {
    opacity: 0;
  }
  .sheet-enter-from .sheet,
  .sheet-leave-to .sheet {
    translate: 0 100%;
  }
  .sheet-enter-active,
  .sheet-leave-active {
    transition: opacity 0.25s;
  }
  .server {
    max-width: 100%;
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
