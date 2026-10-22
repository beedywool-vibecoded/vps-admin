<script setup lang="ts">
import { computed } from 'vue'

export type Tone = 'ok' | 'warn' | 'bad' | 'idle'
const props = defineProps<{ tone: Tone; label: string }>()
const color = computed(() => ({ ok: 'var(--ok)', warn: 'var(--warn)', bad: 'var(--bad)', idle: 'var(--outline)' })[props.tone])
</script>

<template>
  <span role="img" :aria-label="label" v-tip="label" class="dot" :style="{ '--c': color }" />
</template>

<style scoped>
.dot {
  width: 12px;
  height: 12px;
  border-radius: 50%;
  flex: none;
  background: var(--c);
  box-shadow: 0 0 0 4px color-mix(in srgb, var(--c) 20%, transparent);
}
</style>
