<script setup lang="ts">
// Soft Material 3 Expressive shapes drifting behind the content. Purely decorative:
// fixed, below everything, ignored by the pointer and by screen readers, and still when
// the system asks for reduced motion.
import { shapePath, type ShapeKind } from '@/lib/shapes'

const SHAPES: { kind: ShapeKind; size: number; x: string; y: string; color: string; dur: number; delay: number }[] = [
  { kind: 'cookie', size: 460, x: '-8%', y: '-10%', color: 'var(--pc)', dur: 95, delay: 0 },
  { kind: 'clover', size: 360, x: '78%', y: '8%', color: 'var(--tc)', dur: 120, delay: -30 },
  { kind: 'sunny', size: 300, x: '62%', y: '68%', color: 'var(--secc)', dur: 105, delay: -60 },
  { kind: 'flower', size: 400, x: '6%', y: '62%', color: 'var(--secc)', dur: 130, delay: -15 },
  { kind: 'cookie', size: 220, x: '40%', y: '30%', color: 'var(--pc)', dur: 85, delay: -45 },
]
</script>

<template>
  <div class="backdrop" aria-hidden="true">
    <svg
      v-for="(s, i) in SHAPES"
      :key="i"
      class="blob"
      viewBox="0 0 100 100"
      :style="{ width: `${s.size}px`, left: s.x, top: s.y, animationDuration: `${s.dur}s, ${s.dur * 1.7}s`, animationDelay: `${s.delay}s` }"
    >
      <path :d="shapePath(s.kind)" :fill="s.color" />
    </svg>
  </div>
</template>

<style scoped>
.backdrop {
  position: fixed;
  inset: 0;
  z-index: -1;
  overflow: hidden;
  pointer-events: none;
}
.blob {
  position: absolute;
  opacity: 0.32;
  animation-name: drift, spin;
  animation-timing-function: ease-in-out, linear;
  animation-iteration-count: infinite;
  animation-direction: alternate, normal;
}
:global(:root[data-theme='light']) .blob {
  opacity: 0.55;
}
@keyframes drift {
  0% {
    translate: 0 0;
  }
  33% {
    translate: 6vw 4vh;
  }
  66% {
    translate: -3vw 9vh;
  }
  100% {
    translate: 4vw -5vh;
  }
}
@keyframes spin {
  to {
    rotate: 360deg;
  }
}
@media (prefers-reduced-motion: reduce) {
  .blob {
    animation: none;
  }
}
</style>
