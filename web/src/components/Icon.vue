<script setup lang="ts">
// Inline stroke icons, no icon font to download
const PATHS = {
  overview: 'M4 4h7v7H4zM13 4h7v4h-7zM13 10h7v10h-7zM4 13h7v7H4z',
  assistant: 'M12 3l1.8 4.6L18.5 9l-4.7 1.6L12 15l-1.8-4.4L5.5 9l4.7-1.4zM19 15l.9 2.1L22 18l-2.1.9L19 21l-.9-2.1L16 18l2.1-.9zM5 15l.6 1.4L7 17l-1.4.6L5 19l-.6-1.4L3 17l1.4-.6z',
  send: 'M12 19V5M5 12l7-7 7 7',
  terminal: 'M4 5h16a1 1 0 0 1 1 1v12a1 1 0 0 1-1 1H4a1 1 0 0 1-1-1V6a1 1 0 0 1 1-1zM7 10l3 2-3 2M13 15h4',
  power: 'M12 3v9M6.3 6.3a8 8 0 1 0 11.4 0',
  apps: 'M12 2c3 2 5 6 5 10l-2 5H9l-2-5c0-4 2-8 5-10zM12 8.5h.01M9 17l-2 4M15 17l2 4',
  branch: 'M6 3v12M18 9a3 3 0 1 0 0-6 3 3 0 0 0 0 6zM6 21a3 3 0 1 0 0-6 3 3 0 0 0 0 6zM18 9a9 9 0 0 1-9 9',
  lock: 'M6 11h12v10H6zM8 11V8a4 4 0 0 1 8 0v3',
  services: 'M4 6h16M4 12h16M4 18h16',
  containers: 'M3 8l9-5 9 5v8l-9 5-9-5zM3 8l9 5 9-5M12 13v8',
  security: 'M12 3 5 6v5c0 4.5 3 8 7 10 4-2 7-5.5 7-10V6l-7-3z',
  updates: 'M12 4v10M7 9l5 5 5-5M5 20h14',
  settings:
    'M12 9a3 3 0 1 0 0 6 3 3 0 0 0 0-6zM19 12l2-1-1-3-2 .3-1.4-1.4L17 5l-3-1-1 2h-2L10 4 7 5l.4 2.2L6 8.6 4 8.3 3 11l2 1-2 1 1 3 2-.3 1.4 1.4L7 19l3 1 1-2h2l1 2 3-1-.4-2.2 1.4-1.4 2 .3 1-3z',
  restart: 'M20 11a8 8 0 1 0-2.3 5.7M20 4v7h-7',
  play: 'M7 5v14l12-7z',
  logs: 'M4 6h16M4 12h10M4 18h13',
  search: 'M11 4a7 7 0 1 0 0 14 7 7 0 0 0 0-14zM20 20l-3.5-3.5',
  chevron: 'm6 9 6 6 6-6',
  'chevron-right': 'm9 6 6 6-6 6',
  back: 'M19 12H5M11 6l-6 6 6 6',
  plus: 'M12 5v14M5 12h14',
  close: 'M6 6l12 12M18 6 6 18',
  check: 'm5 12 5 5 9-10',
  alert: 'M12 3 2 20h20L12 3zM12 10v4M12 17h.01',
  info: 'M12 3a9 9 0 1 0 0 18 9 9 0 0 0 0-18zM12 11v6M12 7.5h.01',
  key: 'M14 4a6 6 0 1 0 1.8 11.7L18 18v3h3v-3l-5.2-5.2A6 6 0 0 0 14 4zM14 8.5h.01',
  logout: 'M15 4h3a2 2 0 0 1 2 2v12a2 2 0 0 1-2 2h-3M10 17l-5-5 5-5M5 12h11',
  phone: 'M8 2h8a2 2 0 0 1 2 2v16a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2zM11 18h2',
  globe: 'M12 3a9 9 0 1 0 0 18 9 9 0 0 0 0-18zM3 12h18M12 3c3 3 3 15 0 18M12 3c-3 3-3 15 0 18',
  trash: 'M4 7h16M10 11v6M14 11v6M6 7l1 13h10l1-13M9 7V4h6v3',
  external: 'M7 17 17 7M8 7h9v9',
  copy: 'M9 9h10v12H9zM5 15H4V3h12v1',
  sun: 'M12 8a4 4 0 1 0 0 8 4 4 0 0 0 0-8zM12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4',
  history: 'M3 12a9 9 0 1 0 3-6.7M3 4v5h5M12 7v5l3 3',
  folder: 'M3 6a1 1 0 0 1 1-1h5l2 2h9a1 1 0 0 1 1 1v10a1 1 0 0 1-1 1H4a1 1 0 0 1-1-1z',
  person: 'M12 4a4 4 0 1 0 0 8 4 4 0 0 0 0-8zM4 21a8 8 0 0 1 16 0',
  edit: 'M4 20h4L19 9l-4-4L4 16zM13 7l4 4',
  database: 'M4 6c0-1.7 3.6-3 8-3s8 1.3 8 3-3.6 3-8 3-8-1.3-8-3zM4 6v12c0 1.7 3.6 3 8 3s8-1.3 8-3V6M4 12c0 1.7 3.6 3 8 3s8-1.3 8-3',
  game: 'M7 8h10a5 5 0 0 1 0 10c-1.5 0-2.5-1-3.5-2h-3c-1 1-2 2-3.5 2a5 5 0 0 1 0-10zM8 11v4M6 13h4M15.5 12h.01M17.5 14h.01',
  music: 'M9 18V5l11-2v13M9 18a3 3 0 1 1-6 0 3 3 0 0 1 6 0zM20 16a3 3 0 1 1-6 0 3 3 0 0 1 6 0z',
  film: 'M4 4h16v16H4zM8 4v16M16 4v16M4 8h4M4 12h4M4 16h4M16 8h4M16 12h4M16 16h4',
  chat: 'M4 5h16v11H9l-5 4z',
  cloud: 'M7 18a5 5 0 0 1-.5-10A6 6 0 0 1 18 9a4.5 4.5 0 0 1-.5 9z',
  mail: 'M3 6h18v12H3zM3 7l9 6 9-6',
  cart: 'M3 4h2l2.5 11h11L21 7H6.2M9 20h.01M18 20h.01',
  code: 'M8 7l-5 5 5 5M16 7l5 5-5 5M14 4l-4 16',
  robot: 'M5 9h14v10H5zM12 9V5M12 4h.01M9 13h.01M15 13h.01M9 16h6M3 13v3M21 13v3',
  chart: 'M4 20V4M4 20h16M8 16v-5M12 16V8M16 16v-3',
  camera: 'M4 8h3l2-3h6l2 3h3v11H4zM12 11a3 3 0 1 0 0 6 3 3 0 0 0 0-6z',
  book: 'M4 5a2 2 0 0 1 2-2h14v16H6a2 2 0 0 0-2 2zM4 5v16M8 7h8',
  home: 'M3 11l9-7 9 7M5 10v10h14V10M10 20v-6h4v6',
  bolt: 'M13 2 4 14h7l-1 8 9-12h-7z',
  heart: 'M12 20s-7-4.4-9-9a5 5 0 0 1 9-3 5 5 0 0 1 9 3c-2 4.6-9 9-9 9z',
  star: 'M12 3l2.7 5.6 6.1.9-4.4 4.3 1 6.1L12 17l-5.4 2.9 1-6.1L3.2 9.5l6.1-.9z',
  leaf: 'M5 19C5 10 10 5 20 4c-1 10-6 15-15 15zM5 19l7-7',
  paw: 'M12 13c-3 0-6 3-6 5.5S8 21 12 21s6-.5 6-2.5S15 13 12 13zM5 9a1.5 2 0 1 0 3 0 1.5 2 0 1 0-3 0zM16 9a1.5 2 0 1 0 3 0 1.5 2 0 1 0-3 0zM8.5 5a1.5 2 0 1 0 3 0 1.5 2 0 1 0-3 0zM12.5 5a1.5 2 0 1 0 3 0 1.5 2 0 1 0-3 0z',
  coffee: 'M4 9h12v6a5 5 0 0 1-5 5H9a5 5 0 0 1-5-5zM16 11h2a2 2 0 0 1 0 4h-2M8 3v3M12 3v3',
  image: 'M4 4h16v16H4zM4 16l5-5 4 4 3-3 4 4M15 8h.01',
  video: 'M3 6h13v12H3zM16 10l5-3v10l-5-3',
  bell: 'M6 16v-5a6 6 0 0 1 12 0v5l2 2H4zM10 21h4',
  cpu: 'M7 7h10v10H7zM10 10h4v4h-4zM9 3v4M15 3v4M9 17v4M15 17v4M3 9h4M3 15h4M17 9h4M17 15h4',
  wrench: 'M14.5 6.5a4 4 0 0 0-5.3 5.3L4 17v3h3l5.2-5.2a4 4 0 0 0 5.3-5.3l-2.5 2.5-2.5-.5-.5-2.5z',
  users: 'M9 11a4 4 0 1 0 0-8 4 4 0 0 0 0 8zM2 21a7 7 0 0 1 14 0M16 3.5a4 4 0 0 1 0 7.5M18 14a6 6 0 0 1 4 7',
  calendar: 'M4 5h16v15H4zM4 10h16M8 3v4M16 3v4',
  map: 'M9 4 3 6v14l6-2 6 2 6-2V4l-6 2zM9 4v14M15 6v14',
  server: 'M4 4h16v6H4zM4 14h16v6H4zM8 7h.01M8 17h.01',
  shop: 'M4 9l1.5-5h13L20 9M4 9h16v11H4zM4 9a2.7 2.7 0 0 0 5.3 0 2.7 2.7 0 0 0 5.4 0 2.7 2.7 0 0 0 5.3 0M10 20v-5h4v5',
} as const
const FILLED = {
  github:
    'M12 .3a12 12 0 0 0-3.8 23.4c.6.1.8-.3.8-.6v-2c-3.3.7-4-1.6-4-1.6-.6-1.4-1.4-1.8-1.4-1.8-1-.7.1-.7.1-.7 1.2.1 1.8 1.2 1.8 1.2 1 1.8 2.8 1.3 3.5 1 0-.8.4-1.3.7-1.6-2.7-.3-5.5-1.3-5.5-5.9 0-1.3.5-2.4 1.2-3.2 0-.3-.5-1.5.1-3.2 0 0 1-.3 3.3 1.2a11.5 11.5 0 0 1 6 0C17.3 4.7 18.3 5 18.3 5c.7 1.7.2 2.9.1 3.2.8.8 1.2 1.9 1.2 3.2 0 4.6-2.8 5.6-5.5 5.9.5.4.9 1.1.9 2.2v3.3c0 .3.1.7.8.6A12 12 0 0 0 12 .3',
  more: 'M5 11a1 1 0 1 0 0 2 1 1 0 0 0 0-2zM12 11a1 1 0 1 0 0 2 1 1 0 0 0 0-2zM19 11a1 1 0 1 0 0 2 1 1 0 0 0 0-2z',
  stop: 'M7 6h10a1 1 0 0 1 1 1v10a1 1 0 0 1-1 1H7a1 1 0 0 1-1-1V7a1 1 0 0 1 1-1z' } as const

export type IconName = keyof typeof PATHS | keyof typeof FILLED

withDefaults(defineProps<{ name: IconName; size?: number }>(), { size: 22 })
</script>

<template>
  <svg v-if="name in FILLED" :width="size" :height="size" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
    <path :d="FILLED[name as keyof typeof FILLED]" />
  </svg>
  <svg
    v-else
    :width="size"
    :height="size"
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    stroke-width="2"
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
  >
    <path :d="PATHS[name as keyof typeof PATHS]" />
  </svg>
</template>
