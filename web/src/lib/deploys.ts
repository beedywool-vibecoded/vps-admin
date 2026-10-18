import type { Deployment } from './api'
import type { Tone } from '@/components/StatusDot.vue'

export const BUILD_LABEL: Record<string, string> = {
  auto: 'Detect automatically',
  dockerfile: 'Dockerfile',
  compose: 'Docker Compose',
  node: 'Node app',
  static: 'Static site',
}

export function depTone(d?: Deployment): Tone {
  if (!d) return 'idle'
  if (d.status === 'live') return 'ok'
  if (d.status === 'failed') return 'bad'
  if (d.status === 'superseded' || d.status === 'cancelled') return 'idle'
  return 'warn'
}

export function appTone(status: string, latest?: Deployment): Tone {
  if (latest && ['queued', 'building', 'starting'].includes(latest.status)) return 'warn'
  if (status === 'running') return 'ok'
  if (status === 'degraded' || status === 'restarting' || status === 'missing') return 'bad'
  return 'idle'
}

export const STATUS_LABEL: Record<Deployment['status'], string> = {
  queued: 'Queued',
  building: 'Building',
  starting: 'Starting',
  live: 'Live',
  failed: 'Failed',
  superseded: 'Previous',
  cancelled: 'Cancelled',
}

export function slugify(s: string): string {
  return s
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 40)
}

// Suggests app.example.com from a panel at admin.example.com.
export function suggestDomain(publicUrl: string, name: string): string {
  try {
    const host = new URL(publicUrl).hostname
    const parts = host.split('.')
    if (parts.length < 3 || host === 'localhost') return ''
    return `${name}.${parts.slice(1).join('.')}`
  } catch {
    return ''
  }
}
