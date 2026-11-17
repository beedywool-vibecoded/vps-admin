// Material You on the web: the whole palette is derived from one seed color with the
// same tonal-spot scheme Android uses for wallpaper colors.
import {
  Hct,
  MaterialDynamicColors as C,
  SchemeTonalSpot,
  argbFromHex,
  hexFromArgb,
} from '@material/material-color-utilities'
import { ref, watch } from 'vue'

export const SEEDS = [
  { name: 'Purple', hex: '#6750A4' },
  { name: 'Teal', hex: '#006A6A' },
  { name: 'Blue', hex: '#0061A4' },
  { name: 'Pink', hex: '#984061' },
  { name: 'Green', hex: '#386A20' },
  { name: 'Orange', hex: '#8B5000' },
  { name: 'Red', hex: '#B3261E' },
  { name: 'Indigo', hex: '#4355B9' },
  { name: 'Olive', hex: '#5B6300' },
  { name: 'Slate', hex: '#5B5F72' },
] as const

export type ThemeMode = 'system' | 'dark' | 'light'

function load(key: string, def: string): string {
  try {
    return localStorage.getItem(key) ?? def
  } catch {
    return def
  }
}
function save(key: string, value: string) {
  try {
    localStorage.setItem(key, value)
  } catch {
    // private mode: the choice just lasts for this visit
  }
}

export const seed = ref(load('vpsadmin.seed', SEEDS[0].hex))
export const mode = ref(load('vpsadmin.mode', 'system') as ThemeMode)

const ROLES: Record<string, (typeof C)[keyof typeof C]> = {
  '--surface': C.surface,
  '--scc': C.surfaceContainerLowest,
  '--scl': C.surfaceContainerLow,
  '--sc': C.surfaceContainer,
  '--sch': C.surfaceContainerHigh,
  '--schh': C.surfaceContainerHighest,
  '--primary': C.primary,
  '--on-primary': C.onPrimary,
  '--pc': C.primaryContainer,
  '--on-pc': C.onPrimaryContainer,
  '--secc': C.secondaryContainer,
  '--on-secc': C.onSecondaryContainer,
  '--tertiary': C.tertiary,
  '--tc': C.tertiaryContainer,
  '--on-tc': C.onTertiaryContainer,
  '--error': C.error,
  '--on-error': C.onError,
  '--ec': C.errorContainer,
  '--on-ec': C.onErrorContainer,
  '--on-surface': C.onSurface,
  '--on-sv': C.onSurfaceVariant,
  '--outline': C.outline,
  '--outline-v': C.outlineVariant,
}

// Theme shared by the Launcher (beedywool-vibecoded/launcher): inside its frame the panel follows
// the launcher's seed and mode, live, and its own settings are kept but not used (Settings shows
// "Controlled by an external source"). Opened directly, nothing changes.
export const external = ref<{ seed: string; mode: ThemeMode } | null>(null)
const activeSeed = () => external.value?.seed ?? seed.value
const activeMode = () => external.value?.mode ?? mode.value

const media = typeof window !== 'undefined' ? window.matchMedia('(prefers-color-scheme: dark)') : null

export function isDark(): boolean {
  const m = activeMode()
  return m === 'dark' || (m === 'system' && (media?.matches ?? true))
}

export function applyTheme() {
  const dark = isDark()
  const scheme = new SchemeTonalSpot(Hct.fromInt(argbFromHex(activeSeed())), dark, 0)
  const root = document.documentElement
  root.dataset.theme = dark ? 'dark' : 'light'
  for (const [name, role] of Object.entries(ROLES)) {
    root.style.setProperty(name, hexFromArgb((role as { getArgb(s: unknown): number }).getArgb(scheme)))
  }
  // Commands use a dark surface in light mode too.
  const night = dark ? scheme : new SchemeTonalSpot(Hct.fromInt(argbFromHex(activeSeed())), true, 0)
  const tone = (role: unknown) => hexFromArgb((role as { getArgb(s: unknown): number }).getArgb(night))
  root.style.setProperty('--code-bg', tone(C.surfaceContainerLowest))
  root.style.setProperty('--code-fg', tone(C.onSurface))
  root.style.setProperty('--code-border', tone(C.outlineVariant))
  document.querySelector('meta[name="theme-color"]')?.setAttribute('content', hexFromArgb(C.surface.getArgb(scheme)))
}

export function initTheme() {
  applyTheme()
  media?.addEventListener('change', () => activeMode() === 'system' && applyTheme())
  initThemeBridge()
  watch(external, applyTheme)
  watch([seed, mode], () => {
    save('vpsadmin.seed', seed.value)
    save('vpsadmin.mode', mode.value)
    applyTheme()
  })
}

// Only the page framing this app can send it a theme, and only the launcher may frame it
// (frame-ancestors: launcher_origin in the config). Anything malformed is ignored.
function initThemeBridge() {
  if (typeof window === 'undefined' || window.parent === window) return
  window.addEventListener('message', (e) => {
    if (e.source !== window.parent) return
    const d = e.data as { type?: unknown; v?: unknown; shared?: unknown; seed?: unknown; mode?: unknown } | null
    if (!d || d.type !== 'launcher:theme' || d.v !== 1) return
    if (d.shared === true && typeof d.seed === 'string' && /^#[0-9a-f]{6}$/i.test(d.seed) && (d.mode === 'system' || d.mode === 'light' || d.mode === 'dark'))
      external.value = { seed: d.seed, mode: d.mode }
    else if (d.shared === false) external.value = null
  })
  window.parent.postMessage({ type: 'launcher:hello', v: 1 }, '*')
}
