// Typed client for the agent API. Types mirror the Go JSON in internal/.

export interface AuthState {
  paired: boolean
  hasPasskeys: boolean
  scope: '' | 'enroll' | 'full'
  deviceId?: string
  rpId: string
  secureOrigin: boolean
}

export interface Host {
  hostname: string
  os: string
  kernel: string
  arch: string
  cpuModel: string
  cores: number
}

export interface Disk {
  mount: string
  fs: string
  total: number
  used: number
}

export interface Snapshot {
  time: string
  uptime: number
  cpu: number
  load: [number, number, number]
  memTotal: number
  memUsed: number
  swapTotal: number
  swapUsed: number
  netRx: number
  netTx: number
  disks: Disk[]
}

export interface Point {
  t: number
  cpu: number
  mem: number
  rx: number
  tx: number
  load: number
}

export interface Service {
  name: string
  description: string
  load: string
  active: string
  sub: string
  enabled?: string
}

export type CheckStatus = 'ok' | 'warn' | 'fail' | 'unknown'

export interface Check {
  id: string
  group: 'ssh' | 'network' | 'updates'
  title: string
  status: CheckStatus
  detail: string
  fix?: string
}

export interface Port {
  proto: string
  addr: string
  port: number
  process?: string
  public: boolean
}

export interface Security {
  checks: Check[]
  ports: Port[]
  score: number
  checked: string
}

export interface PortBinding {
  ip?: string
  privatePort: number
  publicPort?: number
  type: string
}

export interface Container {
  id: string
  name: string
  image: string
  state: string
  status: string
  health?: string
  created: string
  ports: PortBinding[]
  project?: string
  exposed: boolean
}

export interface ContainerStats {
  id: string
  cpu: number
  memUsed: number
  memLimit: number
}

export interface Release {
  tag: string
  name: string
  notes: string
  prerelease: boolean
  published: string
  url: string
}

export interface UpdateStatus {
  current: string
  latest?: Release
  available: boolean
  checkedAt?: string
  error?: string
  lastResult?: string
  running: boolean
  auto: boolean
  prerelease: boolean
}

export interface Device {
  id: string
  name: string
  kind: 'web' | 'app'
  createdAt: string
  lastSeenAt?: string
  lastIp?: string
  passkeys: number
}

export interface AuditEntry {
  id: number
  at: string
  deviceId?: string
  device?: string
  ip?: string
  action: string
  target?: string
  ok: boolean
  detail?: string
}

export class ApiError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
  ) {
    super(message)
  }
}

// Called on 401/403 so the app can go back to the login screen.
let onUnauthorized: (() => void) | null = null
export function setUnauthorizedHandler(fn: () => void) {
  onUnauthorized = fn
}

export async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = { Accept: 'application/json' }
  // The agent requires this header on every cookie-authenticated change (CSRF guard).
  if (method !== 'GET') headers['X-VPS-Admin'] = '1'
  let payload: BodyInit | undefined
  if (body !== undefined) {
    headers['Content-Type'] = 'application/json'
    payload = typeof body === 'string' ? body : JSON.stringify(body)
  }
  const res = await fetch(path, { method, headers, body: payload, credentials: 'same-origin' })
  if (res.status === 204) return undefined as T
  const data = await res.json().catch(() => ({}))
  if (!res.ok) {
    const code = (data as { error?: string }).error ?? 'http_' + res.status
    if ((res.status === 401 || code === 'passkey_required') && !path.startsWith('/api/auth/')) {
      onUnauthorized?.()
    }
    throw new ApiError(res.status, code, (data as { message?: string }).message ?? code)
  }
  return data as T
}

export const api = {
  authState: () => request<AuthState>('GET', '/api/auth/state'),
  pair: (code: string, name: string) =>
    request<{ deviceId: string }>('POST', '/api/auth/pair', { code, name, kind: 'web' }),
  logout: () => request<void>('POST', '/api/auth/logout'),

  system: () => request<{ host: Host; snapshot: Snapshot; version: string }>('GET', '/api/system'),
  history: (span: '1h' | '24h') =>
    request<{ span: string; points: Point[] }>('GET', `/api/system/history?span=${span}`),

  services: () => request<{ services: Service[] }>('GET', '/api/services'),
  serviceAction: (name: string, action: string) =>
    request<void>('POST', `/api/services/${encodeURIComponent(name)}/${action}`),
  serviceLogs: (name: string, lines = 300) =>
    request<{ logs: string }>('GET', `/api/services/${encodeURIComponent(name)}/logs?lines=${lines}`),

  security: (refresh = false) => request<Security>('GET', `/api/security${refresh ? '?refresh=1' : ''}`),

  containers: () => request<{ containers: Container[] }>('GET', '/api/docker/containers'),
  containerStats: () => request<{ stats: ContainerStats[] }>('GET', '/api/docker/stats'),
  containerAction: (id: string, action: string) =>
    request<void>('POST', `/api/docker/containers/${encodeURIComponent(id)}/${action}`),
  containerLogs: (id: string, tail = 300) =>
    request<{ logs: string }>('GET', `/api/docker/containers/${encodeURIComponent(id)}/logs?tail=${tail}`),

  update: () => request<UpdateStatus>('GET', '/api/update'),
  checkUpdate: () => request<UpdateStatus>('POST', '/api/update/check'),
  applyUpdate: () => request<{ installed: string }>('POST', '/api/update/apply'),
  updateSettings: (s: { auto?: boolean; prerelease?: boolean }) =>
    request<UpdateStatus>('PUT', '/api/settings/update', s),

  devices: () => request<{ devices: Device[]; current: string }>('GET', '/api/devices'),
  revokeDevice: (id: string) => request<void>('DELETE', `/api/devices/${encodeURIComponent(id)}`),
  audit: (before = 0) => request<{ entries: AuditEntry[] }>('GET', `/api/audit?limit=50&before=${before}`),
}

// Live metrics over server-sent events. Returns a function that closes the stream.
export function streamSystem(onSnapshot: (s: Snapshot) => void): () => void {
  const es = new EventSource('/api/system/stream')
  es.onmessage = (e) => onSnapshot(JSON.parse(e.data) as Snapshot)
  return () => es.close()
}
