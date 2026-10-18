import { reactive } from 'vue'
import { api, type Host, type Snapshot } from './api'
import { rememberCurrent } from './servers'

export const session = reactive<{ host: Host | null; snapshot: Snapshot | null; version: string }>({
  host: null,
  snapshot: null,
  version: '',
})

export async function loadSystem() {
  const s = await api.system()
  session.host = s.host
  session.snapshot = s.snapshot
  session.version = s.version
  rememberCurrent(s.host.hostname)
  document.title = `${s.host.hostname} | VPS Admin`
}
