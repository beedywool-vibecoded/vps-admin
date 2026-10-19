// Icons an admin can give to a group of containers, shared with everyone through the
// server appearance. No icon keeps the Material shape.
import type { IconName } from '@/components/Icon.vue'
import { appearance, type Appearance } from './appearance'
import { request } from './api'

export const GROUP_ICONS: IconName[] = ['database', 'game', 'music', 'film', 'chat', 'cloud', 'mail', 'cart', 'code', 'robot', 'chart', 'camera', 'book', 'home', 'bolt', 'heart', 'star', 'leaf', 'paw', 'coffee', 'image', 'video', 'bell', 'cpu', 'wrench', 'users', 'calendar', 'map', 'server', 'shop', 'globe', 'folder', 'terminal', 'lock', 'key', 'phone', 'apps', 'containers', 'security', 'github']

export async function setGroupIcon(project: string, icon: string) {
  Object.assign(appearance, await request<Appearance>('PUT', '/api/appearance/icons', { project, icon }))
}
