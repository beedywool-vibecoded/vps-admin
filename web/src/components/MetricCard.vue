<script setup lang="ts">
import M3Shape from './M3Shape.vue'
import type { ShapeKind } from '@/lib/shapes'

// compact: the smaller size of the widget bar. Without percent there is no progress line.
defineProps<{ label: string; value: string; percent?: number; sub: string; shape: ShapeKind; tint: string; compact?: boolean }>()
</script>

<template>
  <div class="metric" :class="{ compact }">
    <M3Shape :kind="shape" :size="compact ? 96 : 120" :fill="tint" class="deco" />
    <span class="label">{{ label }}</span>
    <span class="value">{{ value }}</span>
    <div v-if="percent !== undefined" class="track" role="meter" :aria-label="label" aria-valuemin="0" aria-valuemax="100" :aria-valuenow="Math.round(percent)">
      <div class="wave" :style="{ width: `${Math.min(Math.max(percent, 0), 100)}%` }" />
      <div class="stop" />
      <div class="rest" />
    </div>
    <span class="sub">{{ sub }}</span>
  </div>
</template>

<style scoped>
.metric {
  position: relative;
  overflow: hidden;
  border-radius: 28px;
  background: var(--sc);
  padding: 20px 22px;
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.deco {
  position: absolute;
  right: -30px;
  top: -34px;
  opacity: 0.55;
  animation: spin 40s linear infinite;
}
@keyframes spin {
  to {
    rotate: 360deg;
  }
}
.label,
.value,
.track,
.sub {
  position: relative;
}
.label {
  font-size: 15px;
  font-weight: 500;
  color: var(--on-sv);
}
.value {
  font-size: 48px;
  line-height: 1;
}
.track {
  display: flex;
  align-items: center;
  height: 14px;
  gap: 4px;
}
/* M3 Expressive wavy progress indicator */
.wave {
  height: 12px;
  min-width: 4px;
  background: var(--primary);
  -webkit-mask: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='24' height='12'%3E%3Cpath d='M0 6Q6 -1 12 6T24 6' fill='none' stroke='black' stroke-width='4' stroke-linecap='round'/%3E%3C/svg%3E") 0 0/24px 12px repeat-x;
  mask: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='24' height='12'%3E%3Cpath d='M0 6Q6 -1 12 6T24 6' fill='none' stroke='black' stroke-width='4' stroke-linecap='round'/%3E%3C/svg%3E") 0 0/24px 12px repeat-x;
  transition: width 0.6s var(--ease-decel);
}
.stop {
  width: 4px;
  height: 14px;
  border-radius: 2px;
  background: var(--primary);
}
.rest {
  flex: 1;
  height: 4px;
  border-radius: 2px;
  background: var(--outline-v);
}
.sub {
  font-size: 13px;
  color: var(--on-sv);
}
.compact {
  padding: 16px 18px;
  gap: 8px;
  border-radius: 24px;
}
.compact .value {
  font-size: 34px;
}
.compact .label {
  font-size: 14px;
}
.compact .sub {
  font-size: 12px;
}
.compact .deco {
  right: -26px;
  top: -30px;
}
</style>
