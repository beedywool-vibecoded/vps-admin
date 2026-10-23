<script setup lang="ts">
// Pelote's loading indicator: an M3 Expressive shape that changes and turns every 700 ms.
// Static (first shape, no rotation) under prefers-reduced-motion; paused while the tab is hidden.
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { LOADER_SEQUENCE, LOADER_SHAPES, LOADER_STEP_MS, LOADER_TURN_DEG } from '@/lib/loaderShapes'

const props = withDefaults(
  defineProps<{ size?: number; label?: string; contained?: boolean }>(),
  { size: 48, label: 'Loading', contained: false },
)

const reduce = window.matchMedia('(prefers-reduced-motion: reduce)').matches
const step = ref(0)
let timer: ReturnType<typeof setInterval> | undefined

onMounted(() => {
  if (reduce) return
  timer = setInterval(() => {
    if (!document.hidden) step.value = (step.value + 1) % LOADER_SEQUENCE.length
  }, LOADER_STEP_MS)
})
onBeforeUnmount(() => clearInterval(timer))

const icon = computed(() => Math.round(props.size * 0.79))
const d = computed(() => LOADER_SHAPES[LOADER_SEQUENCE[step.value] ?? 'cookie9'])
const turn = computed(() => `${step.value * LOADER_TURN_DEG}deg`)
</script>

<template>
  <span
    class="shape-loader"
    :class="{ contained }"
    :style="{ width: `${size}px`, height: `${size}px` }"
    role="progressbar"
    :aria-label="label"
  >
    <svg :width="icon" :height="icon" viewBox="0 0 100 100" aria-hidden="true" :style="{ transform: `rotate(${turn})` }">
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
svg {
  display: block;
  transition: transform 650ms cubic-bezier(0.34, 1.56, 0.64, 1);
}
@media (prefers-reduced-motion: reduce) {
  svg {
    transition: none;
  }
}
</style>
