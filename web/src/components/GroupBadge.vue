<script setup lang="ts">
// The badge of a container group: its chosen icon, or a Material shape by default.
import { computed } from 'vue'
import Icon, { type IconName } from './Icon.vue'
import M3Shape from './M3Shape.vue'
import { appearance } from '@/lib/appearance'
import type { ShapeKind } from '@/lib/shapes'

const props = defineProps<{ project: string; index: number; size?: number }>()
const SHAPES: ShapeKind[] = ['cookie', 'clover', 'sunny', 'flower']
const icon = computed(() => appearance.groupIcons?.[props.project] as IconName | undefined)
const px = computed(() => props.size ?? 36)
const tint = computed(() => (props.index % 2 ? 'var(--secc)' : 'var(--pc)'))
const ink = computed(() => (props.index % 2 ? 'var(--on-secc)' : 'var(--on-pc)'))
</script>

<template>
  <span v-if="icon" class="badge" :style="{ width: `${px}px`, height: `${px}px`, background: tint, color: ink }">
    <Icon :name="icon" :size="Math.round(px * 0.55)" />
  </span>
  <M3Shape v-else :kind="SHAPES[index % SHAPES.length]!" :size="px" :fill="tint" />
</template>

<style scoped>
.badge {
  display: inline-grid;
  place-items: center;
  flex-shrink: 0;
  border-radius: 30%;
}
</style>
