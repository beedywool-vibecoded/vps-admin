<script setup lang="ts">
// Pelote's loading indicator: an M3 Expressive shape that changes and turns every 700 ms.
// Static (first shape, no rotation) under prefers-reduced-motion; paused while the tab is hidden.
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { LOADER_SEQUENCE, LOADER_SHAPES, LOADER_START_MS, LOADER_STEP_MS } from '@/lib/loaderShapes'

const props = withDefaults(
  defineProps<{ size?: number; label?: string; contained?: boolean }>(),
  { size: 48, label: 'Loading', contained: false },
)

const reduce = window.matchMedia('(prefers-reduced-motion: reduce)').matches
const step = ref(0)
let first: ReturnType<typeof setTimeout> | undefined
let timer: ReturnType<typeof setInterval> | undefined

function advance() {
  if (!document.hidden) step.value = (step.value + 1) % LOADER_SEQUENCE.length
}

onMounted(() => {
  if (reduce) return
  // The first change lands at 300 ms so a short wait already moves; then Pelote's 700 ms cadence.
  first = setTimeout(() => {
    advance()
    timer = setInterval(advance, LOADER_STEP_MS)
  }, LOADER_START_MS)
})
onBeforeUnmount(() => {
  clearTimeout(first)
  clearInterval(timer)
})

const icon = computed(() => Math.round(props.size * 0.79))
const d = computed(() => LOADER_SHAPES[LOADER_SEQUENCE[step.value] ?? 'cookie9'])
</script>

<template>
  <span
    class="shape-loader"
    :class="{ contained }"
    :style="{ width: `${size}px`, height: `${size}px` }"
    role="progressbar"
    :aria-label="label"
  >
    <svg :width="icon" :height="icon" viewBox="0 0 100 100" aria-hidden="true">
      <path :d="d" fill="currentColor" />
    </svg>
  </span>
</template>

<style scoped>
.shape-loader {
  display: inline-grid;
  place-items: center;
  flex: none;
  color: var(--primary);
}
.shape-loader.contained {
  border-radius: 24%;
  background: var(--pc);
  color: var(--on-pc);
}
/* Always turning, like Pelote's ring (ld-turn): it never sits still between shapes. */
svg {
  display: block;
  animation: shape-loader-turn 1.4s linear infinite;
}
@keyframes shape-loader-turn {
  to {
    transform: rotate(360deg);
  }
}
@media (prefers-reduced-motion: reduce) {
  svg {
    animation: none;
  }
}
</style>
