// Locks page scrolling while at least one modal is open. Counted, so nested dialogs
// unlock only when the last one closes. The scrollbar gutter is kept to avoid a jump.
let count = 0

export function lockScroll() {
  if (count++ === 0) {
    const root = document.documentElement
    root.style.scrollbarGutter = 'stable'
    root.style.overflow = 'hidden'
  }
}

export function unlockScroll() {
  if (count > 0 && --count === 0) {
    const root = document.documentElement
    root.style.overflow = ''
    root.style.scrollbarGutter = ''
  }
}
