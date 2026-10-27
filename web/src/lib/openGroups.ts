// Which container groups are expanded, persisted in this browser. Collapsed by default.
import { ref } from 'vue'

const KEY = 'vpsadmin.openGroups'

function read(): string[] {
  try {
    const v = JSON.parse(localStorage.getItem(KEY) ?? '[]')
    return Array.isArray(v) ? v : []
  } catch {
    return []
  }
}

export const openGroups = ref(new Set<string>(read()))

export function toggleGroup(name: string) {
  const next = new Set(openGroups.value)
  if (next.has(name)) next.delete(name)
  else next.add(name)
  openGroups.value = next
  try {
    localStorage.setItem(KEY, JSON.stringify([...next]))
  } catch {
    // storage unavailable: the state lasts for this visit
  }
}
