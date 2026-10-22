<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'

// M3 Expressive wavy progress, ported from Pelote (src/previews/bundle.js): a wave in primary up
// to the value, a 6px gap, a flat track in secondary container and a stop dot. The phase slides
// one wavelength every 1.6 s; it pauses off screen, when the tab is hidden, and it stops under
// prefers-reduced-motion.
const props = defineProps<{ percent: number }>()

const H = 14
const MID = 7
const AMP = 3
const LAMBDA = 24
const GAP = 6
const CYCLE = 1600

const host = ref<HTMLSpanElement | null>(null)
const width = ref(200)
const active = ref('')
const track = ref('')
const stopX = ref(0)

const reduce = window.matchMedia('(prefers-reduced-motion: reduce)').matches
const t0 = performance.now()
let visible = true
let frame = 0
let observer: IntersectionObserver | null = null
let resize: ResizeObserver | null = null

function wave(x0: number, x1: number, phase: number): string {
  if (x1 - x0 < 1) return ''
  let d = ''
  for (let x = x0; x <= x1; x += 2) {
    const y = MID + AMP * Math.sin((x / LAMBDA) * 2 * Math.PI + phase)
    d += `${x === x0 ? 'M' : 'L'}${x.toFixed(1)} ${y.toFixed(2)}`
  }
  return d
}

function line(x0: number, x1: number): string {
  return x1 - x0 < 6 ? '' : `M${x0.toFixed(1)} ${MID}L${x1.toFixed(1)} ${MID}`
}

function draw(now: number) {
  const w = host.value?.clientWidth ?? 0
  if (w < 8) return
  width.value = w
  const phase = reduce ? 0 : ((now - t0) / CYCLE) * 2 * Math.PI * -1
  const l = 2
  const r = w - 2
  const value = Math.min(Math.max(props.percent, 0), 100) / 100
  const end = l + (r - 4 - l) * value
  active.value = wave(l, end, phase)
  track.value = line(end + GAP, r - 6)
  stopX.value = r
}

function loop(now: number) {
  if (visible && !document.hidden) draw(now)
  frame = requestAnimationFrame(loop)
}

watch(() => props.percent, () => draw(performance.now()))

onMounted(() => {
  draw(performance.now())
  if (host.value && 'IntersectionObserver' in window) {
    observer = new IntersectionObserver((entries) => {
      visible = entries[0]?.isIntersecting ?? false
    })
    observer.observe(host.value)
  }
  if (reduce) {
    // No frame loop: follow a width change instead.
    resize = new ResizeObserver(() => draw(performance.now()))
    if (host.value) resize.observe(host.value)
    return
  }
  frame = requestAnimationFrame(loop)
})

onBeforeUnmount(() => {
  cancelAnimationFrame(frame)
  observer?.disconnect()
  resize?.disconnect()
})
</script>

<template>
  <span ref="host" class="wvp">
    <svg :width="width" :height="H" :viewBox="`0 0 ${width} ${H}`" aria-hidden="true">
      <path class="track" :d="track" />
      <path class="active" :d="active" />
      <circle class="stop" :cx="stopX" :cy="MID" r="2" />
    </svg>
  </span>
</template>

<style scoped>
.wvp {
  display: block;
  width: 100%;
}
svg {
  display: block;
}
.track,
.active {
  fill: none;
  stroke-width: 4;
  stroke-linecap: round;
}
.track {
  stroke: var(--secc);
}
.active {
  stroke: var(--primary);
}
.stop {
  fill: var(--primary);
}
</style>
