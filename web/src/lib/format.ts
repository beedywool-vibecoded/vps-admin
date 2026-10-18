const UNITS = ['B', 'KiB', 'MiB', 'GiB', 'TiB']

export function bytes(n: number, digits = 1): string {
  let i = 0
  while (n >= 1024 && i < UNITS.length - 1) {
    n /= 1024
    i++
  }
  return `${n.toFixed(i === 0 ? 0 : digits)} ${UNITS[i]}`
}

export function rate(n: number): string {
  return bytes(n) + '/s'
}

export function pct(n: number): string {
  return `${Math.round(n)}%`
}

export function duration(seconds: number): string {
  const d = Math.floor(seconds / 86400)
  const h = Math.floor((seconds % 86400) / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  if (d >= 7) return `${Math.floor(d / 7)} w ${d % 7} d`
  if (d > 0) return `${d} d ${h} h`
  if (h > 0) return `${h} h ${m} min`
  return `${m} min`
}

const rtf = new Intl.RelativeTimeFormat('en', { numeric: 'auto' })

export function ago(iso?: string): string {
  if (!iso) return 'never'
  const s = (new Date(iso).getTime() - Date.now()) / 1000
  const abs = Math.abs(s)
  if (abs < 60) return 'just now'
  if (abs < 3600) return rtf.format(Math.round(s / 60), 'minute')
  if (abs < 86400) return rtf.format(Math.round(s / 3600), 'hour')
  if (abs < 86400 * 30) return rtf.format(Math.round(s / 86400), 'day')
  return new Date(iso).toLocaleDateString('en', { day: 'numeric', month: 'short', year: 'numeric' })
}

export function dateTime(iso: string): string {
  return new Date(iso).toLocaleString('en', { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' })
}

// Untagged images come back as a full digest; the short form is what `docker ps` shows.
export function image(ref: string): string {
  return ref.startsWith('sha256:') ? ref.slice(7, 19) : ref
}
