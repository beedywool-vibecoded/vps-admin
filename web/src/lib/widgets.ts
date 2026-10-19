// The widget bar on the right of wide screens. Whether it is open and which widgets it
// shows are choices of this browser. While it shows, the Overview leaves out the cards
// it would repeat.
import { computed, ref, watch } from 'vue'

export const WIDGETS = [
  { id: 'cpu', label: 'CPU' },
  { id: 'memory', label: 'Memory' },
  { id: 'swap', label: 'Swap' },
  { id: 'disks', label: 'Disks' },
  { id: 'network', label: 'Network' },
  { id: 'system', label: 'Load and uptime' },
  { id: 'containers', label: 'Containers' },
  { id: 'updates', label: 'Updates' },
] as const
export type WidgetId = (typeof WIDGETS)[number]['id']

function read<T>(key: string, def: T): T {
  try {
    const v = localStorage.getItem(key)
    return v === null ? def : (JSON.parse(v) as T)
  } catch {
    return def
  }
}
function write(key: string, v: unknown) {
  try {
    localStorage.setItem(key, JSON.stringify(v))
  } catch {
    // storage unavailable: the choice lasts for this visit
  }
}

const media = typeof window !== 'undefined' ? window.matchMedia('(min-width: 1500px)') : null
export const wide = ref(media?.matches ?? false)
media?.addEventListener('change', (e) => (wide.value = e.matches))

export const barOpen = ref(read('vpsadmin.widgets.open', true))
export const hidden = ref<string[]>(read('vpsadmin.widgets.hidden', []))
watch(barOpen, (v) => write('vpsadmin.widgets.open', v))
watch(hidden, (v) => write('vpsadmin.widgets.hidden', v), { deep: true })

export const barShown = computed(() => wide.value && barOpen.value)
export const visible = (id: WidgetId) => !hidden.value.includes(id)
export function setVisible(id: WidgetId, on: boolean) {
  hidden.value = on ? hidden.value.filter((h) => h !== id) : [...hidden.value, id]
}
