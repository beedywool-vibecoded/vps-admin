<script setup lang="ts">
// Custom disclosure, in place of <details>/<summary> and their browser triangle.
import { ref } from 'vue'
import Icon from './Icon.vue'

const props = defineProps<{ initiallyOpen?: boolean }>()
const open = ref(props.initiallyOpen ?? false)
const id = `exp-${Math.random().toString(36).slice(2, 8)}`
</script>

<template>
  <div class="expand" :class="{ open }">
    <button type="button" class="head st" :aria-expanded="open" :aria-controls="id" @click="open = !open">
      <slot name="summary" />
      <Icon name="chevron" :size="18" class="chev" />
    </button>
    <div :id="id" class="body" :inert="!open">
      <div class="inner"><slot /></div>
    </div>
  </div>
</template>

<style scoped>
.head {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  border: none;
  background: transparent;
  color: inherit;
  text-align: left;
  font: inherit;
  padding: 6px 10px;
  border-radius: 10px;
}
.chev {
  flex-shrink: 0;
  margin-left: auto;
  transition: rotate 0.25s var(--ease);
}
/* Animates to the content height without measuring it.
   Both rules name the open disclosure as their parent, never just any ancestor: a
   disclosure inside another one took the ancestor's state, so its body stayed at full
   height while its summary said it was closed, and its content could not be selected. */
.body {
  display: grid;
  grid-template-rows: 0fr;
  transition: grid-template-rows 0.3s var(--ease-decel);
}
.expand.open > .body {
  grid-template-rows: 1fr;
}
.expand.open > .head .chev {
  rotate: 180deg;
}
.inner {
  overflow: hidden;
  min-height: 0;
}
</style>
