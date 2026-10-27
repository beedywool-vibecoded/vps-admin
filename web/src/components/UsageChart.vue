<script setup lang="ts">
// CPU and memory over time on one 0-100 axis. The memory line is also dashed and both are
// labeled, so color is never the only cue.
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import type { Point } from '@/lib/api'

const props = defineProps<{ points: Point[]; span: '1h' | '24h' }>()

// Real pixel width, so text is never stretched
const W = ref(800)
const plot = ref<HTMLElement>()
let ro: ResizeObserver | undefined
onMounted(() => {
  ro = new ResizeObserver(([e]) => e && e.contentRect.width > 0 && (W.value = e.contentRect.width))
  if (plot.value) ro.observe(plot.value)
})
onBeforeUnmount(() => ro?.disconnect())
const H = 220
const PAD = { l: 36, r: 56, t: 12, b: 26 }
const iw = computed(() => W.value - PAD.l - PAD.r)
const ih = H - PAD.t - PAD.b

const showTable = ref(false)
const hover = ref<number | null>(null)

const x = (i: number) => PAD.l + (props.points.length <= 1 ? iw.value : (i / (props.points.length - 1)) * iw.value)
const y = (v: number) => PAD.t + ih - (Math.min(Math.max(v, 0), 100) / 100) * ih

function line(key: 'cpu' | 'mem') {
  return props.points.map((p, i) => `${i ? 'L' : 'M'}${x(i).toFixed(1)},${y(p[key]).toFixed(1)}`).join(' ')
}
const cpuPath = computed(() => line('cpu'))
const memPath = computed(() => line('mem'))
const last = computed(() => props.points[props.points.length - 1])

const time = (t: number) =>
  new Date(t * 1000).toLocaleTimeString('en', props.span === '1h' ? { hour: '2-digit', minute: '2-digit', second: '2-digit' } : { hour: '2-digit', minute: '2-digit' })

const ticks = computed(() => {
  const n = props.points.length
  if (n < 2) return []
  const out: { x: number; label: string }[] = []
  for (const f of [0, 0.25, 0.5, 0.75, 1]) {
    const i = Math.round(f * (n - 1))
    const label = new Date(props.points[i]!.t * 1000).toLocaleTimeString('en', { hour: '2-digit', minute: '2-digit' })
    // Skip duplicate labels on short histories.
    if (!out.some((t) => t.label === label)) out.push({ x: x(i), label })
  }
  return out
})

function onMove(e: PointerEvent) {
  const svg = e.currentTarget as SVGSVGElement
  const r = svg.getBoundingClientRect()
  const px = e.clientX - r.left
  const n = props.points.length
  if (n === 0) return
  hover.value = Math.round(Math.min(Math.max((px - PAD.l) / iw.value, 0), 1) * (n - 1))
}
const hp = computed(() => (hover.value === null ? null : props.points[hover.value]))
const tipLeft = computed(() => (hover.value === null ? 0 : Math.min(Math.max(x(hover.value), 90), W.value - 90)))

// Direct labels at the line ends; nudged apart when the two values are close.
const labels = computed(() => {
  if (!last.value) return null
  let cy = y(last.value.cpu)
  let my = y(last.value.mem)
  if (Math.abs(cy - my) < 14) {
    const mid = (cy + my) / 2
    ;[cy, my] = cy <= my ? [mid - 7, mid + 7] : [mid + 7, mid - 7]
  }
  return { cy, my }
})

const tableRows = computed(() => {
  const step = Math.max(1, Math.floor(props.points.length / 24))
  return props.points.filter((_, i) => i % step === 0).reverse()
})
</script>

<template>
  <div class="chart">
    <div class="legend">
      <span><i class="sw s1" />CPU</span>
      <span><i class="sw s2" />Memory</span>
      <span class="spacer" />
      <button class="btn btn-text st" type="button" :aria-pressed="showTable" @click="showTable = !showTable">
        {{ showTable ? 'Chart' : 'Table' }}
      </button>
    </div>

    <p v-if="points.length < 2" class="muted empty">Collecting data, the chart fills in a few seconds.</p>

    <div v-show="points.length >= 2 && !showTable" ref="plot" class="plot">
      <svg
        :viewBox="`0 0 ${W} ${H}`"
        role="img"
        :aria-label="`CPU and memory usage over the last ${span === '1h' ? 'hour' : 'day'}`"
        @pointermove="onMove"
        @pointerleave="hover = null"
      >
        <g class="grid">
          <line v-for="v in [0, 50, 100]" :key="v" :x1="PAD.l" :x2="W - PAD.r" :y1="y(v)" :y2="y(v)" />
        </g>
        <g class="axis">
          <text v-for="v in [0, 50, 100]" :key="v" :x="PAD.l - 8" :y="y(v) + 4" text-anchor="end">{{ v }}%</text>
          <text v-for="t in ticks" :key="t.x" :x="t.x" :y="H - 6" text-anchor="middle">{{ t.label }}</text>
        </g>
        <path :d="memPath" class="ln s2" stroke-dasharray="6 4" />
        <path :d="cpuPath" class="ln s1" />
        <g v-if="labels && last" class="end">
          <text :x="W - PAD.r + 8" :y="labels.cy + 4">{{ Math.round(last.cpu) }}%</text>
          <text :x="W - PAD.r + 8" :y="labels.my + 4">{{ Math.round(last.mem) }}%</text>
        </g>
        <g v-if="hp && hover !== null">
          <line class="cross" :x1="x(hover)" :x2="x(hover)" :y1="PAD.t" :y2="PAD.t + ih" />
          <circle :cx="x(hover)" :cy="y(hp.cpu)" r="5" class="mk s1" />
          <circle :cx="x(hover)" :cy="y(hp.mem)" r="5" class="mk s2" />
        </g>
      </svg>
      <div v-if="hp" class="tip" :style="{ left: `${tipLeft}px` }" role="status">
        <strong>{{ time(hp.t) }}</strong>
        <span><i class="sw s1" />CPU {{ hp.cpu.toFixed(1) }}%</span>
        <span><i class="sw s2" />Memory {{ hp.mem.toFixed(1) }}%</span>
        <span class="muted">Load {{ hp.load.toFixed(2) }}</span>
      </div>
    </div>

    <div v-if="points.length >= 2 && showTable" class="table-wrap">
      <table>
        <thead>
          <tr><th>Time</th><th>CPU</th><th>Memory</th><th>Load</th></tr>
        </thead>
        <tbody>
          <tr v-for="p in tableRows" :key="p.t">
            <td>{{ time(p.t) }}</td>
            <td>{{ p.cpu.toFixed(1) }}%</td>
            <td>{{ p.mem.toFixed(1) }}%</td>
            <td>{{ p.load.toFixed(2) }}</td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>

<style scoped>
.legend {
  display: flex;
  align-items: center;
  gap: 18px;
  font-size: 13px;
  color: var(--on-sv);
}
.legend span {
  display: inline-flex;
  align-items: center;
  gap: 8px;
}
.spacer {
  flex: 1;
}
.sw {
  display: inline-block;
  width: 14px;
  height: 3px;
  border-radius: 2px;
}
.sw.s1 {
  background: var(--series-1);
}
.sw.s2 {
  background: repeating-linear-gradient(90deg, var(--series-2) 0 5px, transparent 5px 8px);
}
.plot {
  position: relative;
}
svg {
  display: block;
  width: 100%;
  height: 220px;
  touch-action: pan-y;
}
.grid line {
  stroke: var(--outline-v);
  stroke-width: 1;
  opacity: 0.6;
  vector-effect: non-scaling-stroke;
}
.axis text,
.end text {
  fill: var(--on-sv);
  font-size: 11px;
}
.end text {
  font-weight: 600;
  fill: var(--on-surface);
}
.ln {
  fill: none;
  stroke-width: 2;
  stroke-linejoin: round;
  stroke-linecap: round;
  vector-effect: non-scaling-stroke;
}
.ln.s1 {
  stroke: var(--series-1);
}
.ln.s2 {
  stroke: var(--series-2);
}
.cross {
  stroke: var(--on-sv);
  stroke-width: 1;
  vector-effect: non-scaling-stroke;
}
.mk {
  stroke: var(--sc);
  stroke-width: 2;
}
.mk.s1 {
  fill: var(--series-1);
}
.mk.s2 {
  fill: var(--series-2);
}
.tip {
  position: absolute;
  top: 4px;
  translate: -50% 0;
  display: flex;
  flex-direction: column;
  gap: 2px;
  padding: 8px 12px;
  border-radius: 12px;
  background: var(--schh);
  font-size: 12px;
  white-space: nowrap;
  pointer-events: none;
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.3);
}
.tip span {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}
.empty {
  margin: 40px 0;
  text-align: center;
}
.table-wrap {
  max-height: 240px;
  overflow: auto;
}
table {
  width: 100%;
  border-collapse: collapse;
  font-size: 13px;
}
th,
td {
  padding: 6px 10px;
  text-align: right;
  border-bottom: 1px solid var(--sch);
}
th:first-child,
td:first-child {
  text-align: left;
}
th {
  color: var(--on-sv);
  font-weight: 500;
  position: sticky;
  top: 0;
  background: var(--sc);
}
</style>
