<script setup lang="ts">
// Navigation rail on wide screens; under 760 px a bottom bar with four sections and, in its middle,
// the large M3 cookie that fans out the seven others (the same shell as Lock, Parcel and Escale,
// app-template's docs/design.md section 6). Icons are outlined, filled on the current section.
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import Icon, { type IconName } from './Icon.vue';
import M3Shape from './M3Shape.vue';
import ServerBackground from './ServerBackground.vue';
import WidgetBar from './WidgetBar.vue';
import { barOpen, barShown, wide } from '@/lib/widgets';
import { loadAppearance } from '@/lib/appearance';
import { streamSystem } from '@/lib/api';
import { loadSystem, session } from '@/lib/session';
import { lockScroll, unlockScroll } from '@/lib/scrollLock';
import { duration } from '@/lib/format';
import { toastError } from '@/lib/ui';

const route = useRoute();
const router = useRouter();

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
];
const MORE = NAV.filter((n) => !n.mobile);
// A section is current on its own path and the pages under it (/apps/3 belongs to /apps)
const isActive = (to: string) =>
    to === '/' ? route.path === '/' : route.path === to || route.path.startsWith(`${to}/`);
const moreActive = computed(() => MORE.some((n) => isActive(n.to)));
// Bottom bar: two sections, the cookie, two sections
const BAR = NAV.filter((n) => n.mobile);
const barLeft = BAR.slice(0, Math.ceil(BAR.length / 2));
const barRight = BAR.slice(Math.ceil(BAR.length / 2));

// The hub's menu (M3 Expressive FAB menu, laid out as a grid): a panel rising from the shape, the
// other sections as tiles, four across (three for six or nine, so no row is left with one)
const cols = computed(() => {
    const n = MORE.length;
    return n <= 4 ? n : n === 6 || n === 9 ? 3 : 4;
});

const moreOpen = ref(false);
const hubBtn = ref<HTMLButtonElement>();
const menu = ref<HTMLElement>();
// The bar stays above the scrim while it fades out (else the cookie darkens)
const fanLeaving = ref(false);
watch(moreOpen, (v) => (v ? lockScroll() : unlockScroll()));
watch(
    () => route.path,
    () => (moreOpen.value = false)
);
// Back (Android's button or gesture, the browser's) closes the menu instead of leaving the page: an
// entry is pushed while it's open, with the router's own state so the router sees the same page
function openMenu() {
    moreOpen.value = true;
    history.pushState({ ...history.state, hubMenu: true }, '');
    void nextTick(() => menu.value?.querySelector<HTMLElement>('a')?.focus({ preventScroll: true }));
}
function closeMenu(refocus = false) {
    if (!moreOpen.value) return;
    moreOpen.value = false;
    if (history.state?.hubMenu) history.back();
    if (refocus) hubBtn.value?.focus({ preventScroll: true });
}
const onPop = () => {
    if (moreOpen.value && !history.state?.hubMenu) moreOpen.value = false;
};
// A section chosen: it takes the menu's history entry, so Back from it returns to the page under
function go(e: MouseEvent, to: string) {
    if (e.metaKey || e.ctrlKey || e.shiftKey || e.button > 0) return;
    e.preventDefault();
    const replace = Boolean(history.state?.hubMenu);
    moreOpen.value = false;
    void (replace ? router.replace(to) : router.push(to));
}
// Arrows move between the tiles (Escape is below)
function menuKey(e: KeyboardEvent) {
    const by: Record<string, number> = { ArrowRight: 1, ArrowLeft: -1, ArrowDown: cols.value, ArrowUp: -cols.value };
    if (!(e.key in by) || !menu.value) return;
    const items = [...menu.value.querySelectorAll<HTMLElement>('a')];
    const i = items.indexOf(document.activeElement as HTMLElement);
    const next = items[Math.min(items.length - 1, Math.max(0, (i < 0 ? 0 : i) + by[e.key]!))];
    if (!next) return;
    e.preventDefault();
    next.focus();
}
const onKey = (e: KeyboardEvent) => {
    if (e.key === 'Escape' && moreOpen.value) closeMenu(true);
};

let stop: (() => void) | undefined;
onMounted(async () => {
    document.addEventListener('keydown', onKey);
    window.addEventListener('popstate', onPop);
    try {
        loadAppearance().catch(() => {});
        await loadSystem();
        stop = streamSystem((s) => (session.snapshot = s));
    } catch (e) {
        toastError(e);
    }
});
onBeforeUnmount(() => {
    document.removeEventListener('keydown', onKey);
    window.removeEventListener('popstate', onPop);
    if (moreOpen.value) unlockScroll();
    stop?.();
});
</script>

<template>
    <div class="shell">
        <ServerBackground />
        <nav class="rail" aria-label="Sections">
            <RouterLink to="/" class="logo" aria-label="VPS Admin home">
                <M3Shape kind="cookie" :size="48" fill="var(--pc)">
                    <path
                        d="M32 40h36M32 50h36M32 60h24"
                        stroke="var(--on-pc)"
                        stroke-width="6"
                        stroke-linecap="round"
                    />
                </M3Shape>
            </RouterLink>
            <RouterLink
                v-for="n in NAV"
                :key="n.to"
                :to="n.to"
                class="nav-item"
                :class="{ active: isActive(n.to) }"
                :aria-current="isActive(n.to) ? 'page' : undefined"
            >
                <span class="pill st"><Icon :name="n.icon" :filled="isActive(n.to)" /></span>
                {{ n.label }}
            </RouterLink>
        </nav>

        <!-- Phone: bottom bar with the large cookie in the middle, which opens the other sections in a fan -->
        <nav class="bar" :class="{ lifted: moreOpen || fanLeaving }" aria-label="Sections">
            <RouterLink
                v-for="n in barLeft"
                :key="n.to"
                :to="n.to"
                class="nav-item"
                :class="{ active: isActive(n.to) }"
                :aria-current="isActive(n.to) ? 'page' : undefined"
            >
                <span class="pill st"><Icon :name="n.icon" :filled="isActive(n.to)" /></span>
                {{ n.label }}
            </RouterLink>
            <span class="hub-slot">
                <button
                    ref="hubBtn"
                    v-tip="moreOpen ? 'Close more sections' : 'More sections'"
                    type="button"
                    class="hub"
                    :class="{ open: moreOpen, cur: moreActive }"
                    :aria-expanded="moreOpen"
                    aria-haspopup="true"
                    aria-controls="hub-menu"
                    :aria-label="moreOpen ? 'Close more sections' : 'More sections'"
                    @click="moreOpen ? closeMenu() : openMenu()"
                >
                    <M3Shape kind="cookie" :size="64" fill="var(--pc)" class="hub-shape" />
                    <Icon name="plus" :size="30" class="hub-icon" />
                </button>
            </span>
            <RouterLink
                v-for="n in barRight"
                :key="n.to"
                :to="n.to"
                class="nav-item"
                :class="{ active: isActive(n.to) }"
                :aria-current="isActive(n.to) ? 'page' : undefined"
            >
                <span class="pill st"><Icon :name="n.icon" :filled="isActive(n.to)" /></span>
                {{ n.label }}
            </RouterLink>
        </nav>

        <Transition name="fan" @before-leave="fanLeaving = true" @after-leave="fanLeaving = false">
            <div v-if="moreOpen" class="scrim" @click.self="closeMenu()">
                <nav
                    id="hub-menu"
                    ref="menu"
                    class="hub-menu"
                    aria-label="More sections"
                    :style="{ '--cols': cols }"
                    @keydown="menuKey"
                >
                    <!-- Plain links: RouterLink would navigate before go() can take the menu's history entry -->
                    <a
                        v-for="(n, k) in MORE"
                        :key="n.to"
                        :href="router.resolve(n.to).href"
                        class="hm-item st"
                        :class="{ active: isActive(n.to) }"
                        :aria-current="isActive(n.to) ? 'page' : undefined"
                        :style="{ '--d': `${k * 25}ms` }"
                        @click="go($event, n.to)"
                    >
                        <span class="hm-tile"><Icon :name="n.icon" :size="26" :filled="isActive(n.to)" /></span>
                        <span class="hm-lbl">{{ n.label }}</span>
                    </a>
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
    /* The rail scrolls on short windows, not the page. */
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
    transition: background 0.2s var(--ease);
}
.nav-item.active {
    color: var(--on-surface);
    font-weight: 600;
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
.bar,
.scrim {
    display: none;
}
.spacer {
    flex: 1;
}

/* Phone: the rail gives way to a bottom bar, the large shape in the middle */
@media (max-width: 760px) {
    .rail {
        display: none;
    }
    .bar {
        position: fixed;
        left: 0;
        right: 0;
        bottom: 0;
        z-index: 10;
        display: flex;
        align-items: center;
        height: calc(64px + env(safe-area-inset-bottom));
        padding: 6px 0 env(safe-area-inset-bottom);
        background: var(--sc);
    }
    /* Arc open: the bar goes above the scrim, under the menus (z-index 30+) */
    .bar.lifted {
        z-index: 29;
    }
    .bar .nav-item {
        width: auto;
        flex: 1;
        min-width: 0;
        gap: 2px;
        padding: 0;
        font-size: 11px;
    }
    .bar .pill {
        width: 52px;
        height: 30px;
    }
    .hub-slot {
        flex: 0 0 84px;
        display: grid;
        place-items: center;
    }
    /* The shape overflows above the bar */
    .hub {
        position: relative;
        display: grid;
        place-items: center;
        width: 64px;
        height: 64px;
        margin-top: -26px;
        padding: 0;
        border: none;
        background: none;
        cursor: pointer;
        border-radius: 50%;
        filter: drop-shadow(0 3px 6px rgba(0, 0, 0, 0.35));
        transition: scale 0.2s var(--ease, ease);
    }
    .hub > * {
        grid-area: 1 / 1;
    }
    .hub-shape {
        transition: rotate 0.5s cubic-bezier(0.34, 1.56, 0.64, 1);
    }
    .hub-icon {
        color: var(--on-pc);
        transition: rotate 0.35s cubic-bezier(0.34, 1.56, 0.64, 1);
    }
    .hub.open .hub-shape {
        rotate: 90deg;
    }
    .hub.open .hub-icon {
        rotate: 45deg;
    }
    .hub:active {
        scale: 0.92;
    }
    /* Section opened from the arc: small dot under the shape */
    .hub.cur::after {
        content: '';
        position: absolute;
        bottom: -9px;
        left: 50%;
        width: 6px;
        height: 6px;
        margin-left: -3px;
        border-radius: 3px;
        background: var(--primary);
    }
    .scrim {
        display: block;
        position: fixed;
        inset: 0;
        z-index: 28;
        background: rgba(0, 0, 0, 0.45);
    }
    /* The hub's menu: a panel rising from the shape (its top is 26 px above the 64 px bar), 16 px
     clear of it, 12 px from the screen's sides at most, scrolling inside if a short screen asks */
    .hub-menu {
        position: absolute;
        left: 50%;
        bottom: calc(64px + 26px + 16px + env(safe-area-inset-bottom));
        display: grid;
        /* Tiles up to 88 px: a short list makes a small panel, a long one shrinks them to fit */
        grid-template-columns: repeat(var(--cols), minmax(0, 88px));
        gap: 4px;
        width: max-content;
        max-width: calc(100% - 24px);
        max-height: calc(100dvh - 64px - 26px - 32px - env(safe-area-inset-bottom));
        overflow-y: auto;
        overscroll-behavior: contain;
        padding: 12px;
        border-radius: 28px;
        background: var(--sc);
        box-shadow: 0 4px 16px rgb(0 0 0 / 0.3);
        translate: -50% 0;
        transform-origin: 50% 100%;
        transition:
            scale 0.42s cubic-bezier(0.34, 1.4, 0.64, 1),
            translate 0.42s cubic-bezier(0.34, 1.4, 0.64, 1),
            opacity 0.2s var(--ease);
    }
    .hm-item {
        display: flex;
        flex-direction: column;
        align-items: center;
        gap: 6px;
        min-width: 0;
        padding: 10px 2px 8px;
        border-radius: 20px;
        color: var(--on-surface);
        font-size: 12px;
        font-weight: 600;
        line-height: 1.25;
        text-align: center;
        text-decoration: none;
        transition:
            opacity 0.32s var(--ease-decel) var(--d),
            translate 0.32s var(--ease-decel) var(--d);
    }
    .hm-tile {
        position: relative;
        display: grid;
        place-items: center;
        width: 56px;
        height: 56px;
        border-radius: 18px;
        background: var(--sch);
        color: var(--on-surface);
        transition:
            border-radius 0.2s var(--ease),
            background 0.2s var(--ease);
    }
    .hm-item:active .hm-tile {
        border-radius: 28px;
    }
    .hm-item.active .hm-tile {
        background: var(--secc);
        color: var(--on-secc);
    }
    .hm-lbl {
        max-width: 100%;
        overflow-wrap: anywhere;
    }
    /* Opening: the panel grows from the shape on a spring, the tiles rise in one after another;
     closing: a quick fade */
    .fan-enter-from,
    .fan-leave-to {
        opacity: 0;
    }
    .fan-enter-from .hub-menu {
        opacity: 0;
        scale: 0.6;
        translate: -50% 40px;
    }
    .fan-enter-from .hm-item {
        opacity: 0;
        translate: 0 10px;
    }
    .fan-leave-to .hub-menu {
        opacity: 0;
        scale: 0.94;
        transition-duration: 0.18s;
        transition-timing-function: var(--ease);
    }
    .fan-leave-active .hm-item {
        transition: none;
    }
    .fan-enter-active,
    .fan-leave-active {
        transition: opacity 0.3s var(--ease);
    }
    .fan-leave-active {
        transition-duration: 0.2s;
    }
    .main-col {
        padding: 16px 16px 96px;
    }
    .brand {
        font-size: 22px;
    }
    .server {
        max-width: 100%;
    }
    .os {
        display: none;
    }
}
@media (prefers-reduced-motion: reduce) {
    .hub-menu,
    .hm-item,
    .hub-shape,
    .hub-icon {
        transition: none;
    }
}
</style>
