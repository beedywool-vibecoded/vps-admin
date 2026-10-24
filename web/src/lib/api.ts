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

/** One domain the reverse proxy publishes, and where it sends traffic. */
export interface ProxySite {
  domains: string[]
  /** Upstream it forwards to, like 127.0.0.1:8080. Empty for files or a redirect. */
  target: string
  file: string
  /** Written by the panel, rewritten on every deploy. */
  managed: boolean
  /** App id of a managed site; empty when the app is gone. */
  id: string
  vpnOnly: boolean
  scheme: 'http' | 'https'
  /** An app's URL turned off from the panel: the snippet is gone, the app keeps running. */
  paused: boolean
}

export interface ProxyReport {
  /** caddy, nginx, or empty when neither answers. */
  engine: string
  root: string
  sites: ProxySite[]
  /** Config files that exist but could not be read. */
  unreadable: string[]
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
  workDir?: string
  service?: string
  app?: string // panel app it was deployed for
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
  /** Where updates come from, owner/name. */
  repo: string
  /** The repository the build looks at when no custom source is set. */
  defaultRepo: string
  prerelease: boolean
}

export interface AlertSettings {
  enabled: boolean
  webhook: string
  disk: number
  error?: string
  errorAt?: number
}

export interface Device {
  id: string
  name: string
  owner: string
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

export type BuildType = 'auto' | 'dockerfile' | 'compose' | 'node' | 'static'

export interface App {
  id: string
  repo: string
  installationId: number
  branch: string
  rootDir: string
  build: BuildType
  port: number
  domain: string
  autoDeploy: boolean
  keep: number
  memoryMb: number
  createdAt: string
  dir: string
  installCmd: string
  buildCmd: string
  startCmd: string
  dockerfile: string
  composeFile: string
  access: AccessMode | ''
  allow: string
  tsPort: number
  envFile: boolean
  envPath: string
}

export type AccessMode = 'public' | 'vpn' | 'tailscale' | 'local'

export interface Tool {
  installed: boolean
  running: boolean
  version?: string
  nginxPlugin?: boolean
}
export interface VPNLink {
  name: string
  addresses: string[]
}
export interface Environment {
  caddy: Tool
  nginx: Tool
  certbot: Tool
  traefik: boolean
  tailscale?: { running: boolean; name: string; ips: string[]; https: boolean; version: string }
  wireguard: VPNLink[]
  zerotier: VPNLink[]
  ufw: string
  nftables: boolean
  docker: string
  checked: string
}
export interface ServerSetup {
  environment: Environment
  panel: { url: string; webhookUrl: string; access: 'public' | 'tailscale' | 'local' }
  sites: boolean
}

export interface Deployment {
  id: number
  appId: string
  sha: string
  message: string
  trigger: 'manual' | 'push' | 'rollback' | 'create'
  status: 'queued' | 'building' | 'starting' | 'live' | 'failed' | 'superseded' | 'cancelled'
  build: string
  image: string
  container: string
  hostPort: number
  error: string
  createdAt: string
  finishedAt?: string
}

export interface AppView extends App {
  url?: string
  accessMode: AccessMode
  status: string
  live?: Deployment
  latest?: Deployment
  busy: boolean
}

export interface EnvVar {
  key: string
  value: string
  secret: boolean
  hasValue?: boolean
  keep?: boolean
}

export interface Installation {
  id: number
  account: string
  type: string
  htmlUrl: string
}

export interface GitHubStatus {
  connected: boolean
  domains: boolean
  publicUrl: string
  app?: { slug: string; name: string; owner: string; htmlUrl: string; installUrl: string }
  installations?: Installation[]
  error?: string
}

export interface Repo {
  fullName: string
  private: boolean
  defaultBranch: string
  language: string
  pushedAt: string
  installationId: number
}

export interface Detection {
  build: BuildType | ''
  port: number
  reason: string
}

export interface AppInput {
  id?: string
  repo?: string
  branch: string
  rootDir: string
  build: BuildType
  port: number
  domain: string
  autoDeploy: boolean
  keep: number
  memoryMb: number
  access?: AccessMode
  allow?: string
  installCmd?: string
  buildCmd?: string
  startCmd?: string
  dockerfile?: string
  composeFile?: string
  env?: EnvVar[]
  envFile?: boolean
  envPath?: string
  deploy?: boolean
}

export interface UpdateCheck {
  current: string
  latest: { sha: string; message: string }
  upToDate: boolean
}

export type ProviderID = 'ollama' | 'gemini' | 'claude-code'

// One AI the panel can talk to: local (Ollama), free online (Gemini), premium (Claude,
// or the Claude Code CLI on a subscription). Ready means it can answer right now; hint
// says what is missing.
export interface AssistantProvider {
  id: ProviderID
  label: string
  kind: 'openai' | 'cli'
  badge: string
  note: string
  needsKey: boolean
  keyHint?: string
  ready: boolean
  active: boolean
  hint?: string
  model: string
  models: { id: string; label: string; hint?: string }[]
  url?: string
  install?: string
}

export interface AssistantSettings {
  provider: ProviderID
  configured: boolean
  keyHint?: string
  model: string
  effort: 'low' | 'medium' | 'high' | 'xhigh' | 'max'
  autoRead: boolean
  hint?: string
  ollamaUrl?: string
  providers: AssistantProvider[]
}

export interface Conversation {
  id: string
  title: string
  state: 'idle' | 'running' | 'awaiting' | 'error'
  error?: string
  createdAt: string
  updatedAt: string
}

// Content blocks as the Anthropic API sends them; only the fields the panel shows.
export type Block =
  | { type: 'text'; text: string }
  | { type: 'thinking'; thinking: string }
  | { type: 'tool_use'; id: string; name: string; input: Record<string, unknown> }
  | { type: 'tool_result'; tool_use_id: string }
  | { type: string; [k: string]: unknown }

export interface ConvMessage {
  seq: number
  role: 'user' | 'assistant'
  content: { role: string; content: Block[] | string }
}

export interface ToolCall {
  id: string
  convId: string
  batch: number
  ord: number
  name: string
  input: string
  summary: string
  kind: 'read' | 'change' | 'blocked'
  status: 'pending' | 'running' | 'done' | 'denied' | 'blocked'
  result: string
  isError: boolean
  createdAt: string
}

export interface Script {
  name: string
  title?: string // display name set in the panel
  target: string
  description: string
  executable: boolean
  missing: boolean
  running: boolean
  dir?: string
}

export interface ScriptRun {
  id: string
  script: string
  args: string
  device?: string
  started: string
  finished?: string
  exitCode?: number
  error?: string
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
  if (res.status === 502 || res.status === 504) {
    // The reverse proxy in front of the agent answered instead of the agent: usually a
    // restart (an update) or a long request it gave up on.
    throw new ApiError(res.status, 'agent_unreachable', 'The agent did not answer. Check that vps-admin is running, then try again.')
  }
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

/** One encrypted archive on the Drive. */
export interface BackupArchive {
  name: string
  size: number
  time: string
}

export interface BackupLast {
  at: string
  kind: string
  file: string
  size: number
  ok: boolean
  error?: string
}

export interface BackupStatus {
  configured: boolean
  enabled: boolean
  hasToken: boolean
  hasPassphrase: boolean
  path: string
  hour: number
  keep: number
  /** What the daily run writes: the panel state, or everything. */
  kind: 'settings' | 'full'
  mount: boolean
  mountPoint: string
  mounted: boolean
  helper: boolean
  /** rclone's version line, empty when it is not installed. */
  rclone: string
  running: boolean
  progress: string
  restoreStaged: boolean
  last?: BackupLast
  archives: BackupArchive[]
  warning?: string
}

export interface BackupOptions {
  enabled?: boolean
  path?: string
  hour?: number
  keep?: number
  kind?: 'settings' | 'full'
  mount?: boolean
  token?: string
  passphrase?: string
}

export const api = {
  authState: () => request<AuthState>('GET', '/api/auth/state'),
  pair: (code: string, name: string, owner: string) =>
    request<{ deviceId: string }>('POST', '/api/auth/pair', { code, name, owner, kind: 'web' }),
  renameDevice: (id: string, owner: string, name: string) => request<void>('PATCH', `/api/devices/${id}`, { owner, name }),
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
  proxy: () => request<ProxyReport>('GET', '/api/proxy'),
  proxyPause: (app: string, paused: boolean) => request<ProxyReport>('POST', '/api/proxy/pause', { app, paused }),
  proxyOrder: (order: string[]) => request<ProxyReport>('POST', '/api/proxy/order', { order }),
  proxySite: (file: string, action: 'pause' | 'resume' | 'delete' | 'https', domain?: string) =>
    request<ProxyReport>('POST', '/api/proxy/site', { file, action, domain }),

  containers: () => request<{ containers: Container[] }>('GET', '/api/docker/containers'),
  containerStats: () => request<{ stats: ContainerStats[] }>('GET', '/api/docker/stats'),
  containerRestartPolicies: (ids: string[]) =>
    request<{ policies: Record<string, string> }>('GET', `/api/docker/restart?ids=${ids.map(encodeURIComponent).join(',')}`),
  setContainerRestart: (ids: string[], start: boolean) =>
    request<{ policies: Record<string, string> }>('POST', '/api/docker/restart', { ids, start }),
  containerAction: (id: string, action: string) =>
    request<void>('POST', `/api/docker/containers/${encodeURIComponent(id)}/${action}`),
  containerLogs: (id: string, tail = 300) =>
    request<{ logs: string }>('GET', `/api/docker/containers/${encodeURIComponent(id)}/logs?tail=${tail}`),

  update: () => request<UpdateStatus>('GET', '/api/update'),
  checkUpdate: () => request<UpdateStatus>('POST', '/api/update/check'),
  applyUpdate: () => request<{ installed: string }>('POST', '/api/update/apply'),
  updateSettings: (s: { auto?: boolean; prerelease?: boolean; repo?: string }) =>
    request<UpdateStatus>('PUT', '/api/settings/update', s),
  alerts: () => request<AlertSettings>('GET', '/api/alerts'),
  updateAlerts: (s: { enabled?: boolean; webhook?: string; disk?: number }) =>
    request<AlertSettings>('PUT', '/api/alerts', s),
  testAlerts: (webhook: string) => request<{ sent: boolean }>('POST', '/api/alerts/test', { webhook }),

  backup: () => request<BackupStatus>('GET', '/api/backup'),
  backupArchives: () => request<BackupStatus>('GET', '/api/backup?archives=1'),
  backupSave: (o: BackupOptions) => request<BackupStatus>('PUT', '/api/backup', o),
  backupTest: () => request<{ ok: boolean; detail: string }>('POST', '/api/backup/test'),
  backupMount: (mounted: boolean) => request<BackupStatus>('POST', '/api/backup/mount', { mounted }),
  backupRun: (kind: 'settings' | 'full') => request<{ started: boolean }>('POST', '/api/backup/run', { kind }),
  backupRestore: (file: string, passphrase: string) =>
    request<{ restarting: boolean }>('POST', '/api/backup/restore', { file, passphrase }),

  devices: () => request<{ devices: Device[]; current: string }>('GET', '/api/devices'),
  revokeDevice: (id: string) => request<void>('DELETE', `/api/devices/${encodeURIComponent(id)}`),
  environment: () => request<ServerSetup>('GET', '/api/system/environment'),
  reboot: (confirm: string) => request<{ rebooting: boolean }>('POST', '/api/system/reboot', { confirm }),
  scripts: () => request<{ scripts: Script[]; runs: ScriptRun[]; dir: string }>('GET', '/api/scripts'),
  renameScript: (name: string, title: string) => request<void>('PUT', `/api/scripts/${encodeURIComponent(name)}/title`, { title }),
  runScript: (name: string, args: string) => request<{ id: string }>('POST', `/api/scripts/${encodeURIComponent(name)}/run`, { args }),
  stopScript: (name: string) => request<void>('POST', `/api/scripts/${encodeURIComponent(name)}/stop`),
  scriptRunOutput: (id: string) => request<{ output: string }>('GET', `/api/script-runs/${encodeURIComponent(id)}`),
  audit: (before = 0) => request<{ entries: AuditEntry[] }>('GET', `/api/audit?limit=50&before=${before}`),

  github: () => request<GitHubStatus>('GET', '/api/github'),
  githubManifest: (org = '') => request<{ url: string; manifest: string }>('POST', '/api/github/manifest', { org }),
  githubDisconnect: () => request<void>('DELETE', '/api/github'),
  repos: () => request<{ repos: Repo[] }>('GET', '/api/github/repos'),
  branches: (installationId: number, repo: string) =>
    request<{ branches: string[] }>('GET', `/api/github/branches?installation=${installationId}&repo=${encodeURIComponent(repo)}`),
  detect: (r: Repo, ref: string, root: string) =>
    request<{ detection: Detection; files: string[] }>(
      'GET',
      `/api/github/detect?installation=${r.installationId}&repo=${encodeURIComponent(r.fullName)}&ref=${encodeURIComponent(ref)}&root=${encodeURIComponent(root)}`,
    ),

  apps: () => request<{ apps: AppView[]; domains: boolean }>('GET', '/api/apps'),
  app: (id: string) =>
    request<{ app: AppView; env: EnvVar[]; deployments: Deployment[]; domains: boolean }>('GET', `/api/apps/${id}`),
  createApp: (a: AppInput) => request<{ app: App; deployment?: Deployment }>('POST', '/api/apps', a),
  updateApp: (id: string, a: AppInput) => request<{ app: App }>('PUT', `/api/apps/${id}`, a),
  deleteApp: (id: string, volumes: boolean) => request<void>('DELETE', `/api/apps/${id}${volumes ? '?volumes=1' : ''}`),
  setEnv: (id: string, env: EnvVar[], opts?: { envFile?: boolean; envPath?: string }) =>
    request<void>('PUT', `/api/apps/${id}/env`, { env, ...opts }),
  deploy: (id: string) => request<Deployment>('POST', `/api/apps/${id}/deploy`),
  checkApp: (id: string) => request<UpdateCheck>('GET', `/api/apps/${id}/check`),
  inspectFolder: (project: string) =>
    request<{ dir: string; folder: { repo: string; branch: string; head: string } }>('GET', `/api/folders/inspect?project=${encodeURIComponent(project)}`),
  linkFolder: (l: { project: string; repo: string; branch: string; rootDir: string; autoDeploy: boolean }) =>
    request<{ app: App }>('POST', '/api/apps/link', l),
  rollback: (id: string, dep: number) => request<Deployment>('POST', `/api/apps/${id}/rollback/${dep}`),
  restartApp: (id: string) => request<void>('POST', `/api/apps/${id}/restart`),
  assistant: () => request<{ settings: AssistantSettings; models: { id: string; label: string }[] }>('GET', '/api/assistant'),
  assistantUpdate: (s: {
    provider?: ProviderID
    apiKey?: string
    model?: string
    effort?: string
    autoRead?: boolean
    ollamaUrl?: string
  }) => request<{ settings: AssistantSettings; models: { id: string; label: string }[] }>('PUT', '/api/assistant', s),
  conversations: () => request<{ conversations: Conversation[] }>('GET', '/api/assistant/conversations'),
  conversation: (id: string) =>
    request<{ conversation: Conversation; messages: ConvMessage[]; toolCalls: ToolCall[]; running: boolean }>('GET', `/api/assistant/conversations/${id}`),
  newConversation: (message: string) => request<{ conversation: Conversation }>('POST', '/api/assistant/conversations', { message }),
  sendMessage: (id: string, message: string) => request<void>('POST', `/api/assistant/conversations/${id}/messages`, { message }),
  stopConversation: (id: string) => request<void>('POST', `/api/assistant/conversations/${id}/stop`),
  deleteConversation: (id: string) => request<void>('DELETE', `/api/assistant/conversations/${id}`),
  decide: (call: string, approve: boolean) => request<ToolCall>('POST', `/api/assistant/tools/${call}`, { approve }),
  deploymentLog: (id: string, dep: number) =>
    request<{ deployment: Deployment; log: string }>('GET', `/api/apps/${id}/deployments/${dep}`),
}

// Live metrics over server-sent events. Returns a function that closes the stream.
export function streamSystem(onSnapshot: (s: Snapshot) => void): () => void {
  const es = new EventSource('/api/system/stream')
  es.onmessage = (e) => onSnapshot(JSON.parse(e.data) as Snapshot)
  return () => es.close()
}

// Build output of a deployment: the stored log first, then live chunks until it ends.
export function streamDeployment(
  app: string,
  dep: number,
  on: { log: (chunk: string) => void; status: (d: Deployment) => void; end: (d: Deployment) => void },
): () => void {
  const es = new EventSource(`/api/apps/${app}/deployments/${dep}/stream`)
  es.addEventListener('log', (e) => on.log(JSON.parse((e as MessageEvent).data)))
  es.addEventListener('status', (e) => on.status(JSON.parse((e as MessageEvent).data)))
  es.addEventListener('end', (e) => {
    on.end(JSON.parse((e as MessageEvent).data))
    es.close()
  })
  return () => es.close()
}

export interface AssistantEvents {
  message: (m: ConvMessage) => void
  text: (d: { index: number; text: string }) => void
  thinking: (d: { index: number; text: string }) => void
  tool: (t: ToolCall) => void
  state: (s: { state: Conversation['state']; error: string }) => void
}

export function streamConversation(id: string, on: AssistantEvents): () => void {
  const es = new EventSource(`/api/assistant/conversations/${id}/stream`)
  for (const type of ['message', 'text', 'thinking', 'tool', 'state'] as const) {
    es.addEventListener(type, (e) => (on[type] as (v: unknown) => void)(JSON.parse((e as MessageEvent).data)))
  }
  return () => es.close()
}

// Live output of a running script: what it printed so far, then new chunks, then the end.
export function streamScript(name: string, on: { output: (chunk: string) => void; end: (r: { exitCode?: number; error?: string }) => void }): () => void {
  const es = new EventSource(`/api/scripts/${encodeURIComponent(name)}/stream`)
  es.addEventListener('output', (e) => on.output(JSON.parse((e as MessageEvent).data)))
  es.addEventListener('end', (e) => {
    on.end(JSON.parse((e as MessageEvent).data))
    es.close()
  })
  es.onerror = () => es.close()
  return () => es.close()
}

// Rebuilds the compose service of a container and hands over its output as it comes. Resolves with
// whether it succeeded; the server goes on if the page is closed.
export async function rebuildContainer(id: string, output: (text: string) => void): Promise<boolean> {
  const res = await fetch(`/api/docker/containers/${encodeURIComponent(id)}/rebuild`, {
    method: 'POST',
    headers: { 'X-VPS-Admin': '1' },
    credentials: 'same-origin',
  })
  if (!res.ok || !res.body) {
    const data = await res.json().catch(() => ({}))
    throw new ApiError(res.status, data.error ?? 'http_' + res.status, data.message ?? data.error ?? 'Rebuild failed')
  }
  const reader = res.body.pipeThrough(new TextDecoderStream()).getReader()
  let all = ''
  for (;;) {
    const { value, done } = await reader.read()
    if (done) break
    all += value
    output(value)
  }
  return all.trimEnd().endsWith('==> Done')
}
