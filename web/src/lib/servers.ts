// Servers this browser knows. A passkey only works on the origin that created it, so
// each server keeps its own panel; switching means opening the other panel.
import { ref } from 'vue'

export interface KnownServer {
  url: string // origin, e.g. https://admin.example.com
  name: string
  lastSeen: number
}

const KEY = 'vpsadmin.servers'

function read(): KnownServer[] {
  try {
    const list = JSON.parse(localStorage.getItem(KEY) ?? '[]') as KnownServer[]
    return Array.isArray(list) ? list.filter((s) => typeof s.url === 'string') : []
  } catch {
    return []
  }
}

export const servers = ref<KnownServer[]>(read())

function write() {
  try {
    localStorage.setItem(KEY, JSON.stringify(servers.value))
  } catch {
    // storage unavailable: the list lasts for this visit
  }
}

export function rememberCurrent(name: string) {
  const url = location.origin
  const rest = servers.value.filter((s) => s.url !== url)
  servers.value = [{ url, name, lastSeen: Date.now() }, ...rest]
  write()
}

export function forget(url: string) {
  servers.value = servers.value.filter((s) => s.url !== url)
  write()
}

// Accepts "admin.example.com" or a full URL and returns its origin, or null.
export function normalizeServerURL(input: string): string | null {
  const v = input.trim()
  if (!v) return null
  try {
    const u = new URL(/^https?:\/\//.test(v) ? v : 'https://' + v)
    return u.origin
  } catch {
    return null
  }
}
