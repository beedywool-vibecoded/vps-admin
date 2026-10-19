// The server-wide background, set by an administrator and seen by everyone.
import { reactive } from 'vue'
import { request } from './api'

export interface Appearance {
  background: boolean
  opacity: number
  version: string
}

export const appearance = reactive<Appearance>({ background: false, opacity: 0.35, version: '' })

export async function loadAppearance() {
  Object.assign(appearance, await request<Appearance>('GET', '/api/appearance'))
}

export async function setOpacity(opacity: number) {
  Object.assign(appearance, await request<Appearance>('PUT', '/api/appearance', { opacity }))
}

export async function uploadBackground(file: File) {
  const res = await fetch('/api/appearance/background', {
    method: 'PUT',
    headers: { 'X-VPS-Admin': '1', 'Content-Type': file.type || 'application/octet-stream' },
    body: file,
    credentials: 'same-origin',
  })
  const data = await res.json().catch(() => ({}))
  if (!res.ok) throw new Error((data as { message?: string }).message ?? 'Upload failed')
  Object.assign(appearance, data)
}

export async function removeBackground() {
  Object.assign(appearance, await request<Appearance>('DELETE', '/api/appearance/background'))
}
