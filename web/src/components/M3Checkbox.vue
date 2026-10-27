<script setup lang="ts">
const checked = defineModel<boolean>({ required: true })
defineProps<{ label?: string; disabled?: boolean }>()
</script>

<template>
  <button
    type="button"
    role="checkbox"
    class="cb st"
    :class="{ on: checked }"
    :aria-checked="checked"
    :disabled="disabled"
    @click="checked = !checked"
  >
    <span class="box" aria-hidden="true">
      <svg viewBox="0 0 24 24" width="18" height="18"><path d="m5 12.5 4.5 4.5L19 7.5" /></svg>
    </span>
    <span v-if="label || $slots.default" class="txt"><slot>{{ label }}</slot></span>
  </button>
</template>

<style scoped>
.cb {
  display: inline-flex;
  align-items: center;
  gap: 10px;
  min-height: 40px;
  padding: 0 10px 0 4px;
  border: none;
  border-radius: 20px;
  background: transparent;
  color: var(--on-surface);
  font-size: 14px;
  text-align: left;
}
.box {
  width: 18px;
  height: 18px;
  margin: 11px;
  border-radius: 3px;
  /* Ring drawn inside: a border would leave 14px for the 18px tick. */
  box-shadow: inset 0 0 0 2px var(--on-sv);
  display: grid;
  place-items: center;
  flex-shrink: 0;
  transition:
    background 0.15s var(--ease),
    box-shadow 0.15s var(--ease);
}
.box svg {
  fill: none;
  stroke: var(--on-primary);
  stroke-width: 3;
  stroke-linecap: round;
  stroke-linejoin: round;
  stroke-dasharray: 24;
  stroke-dashoffset: 24;
  transition: stroke-dashoffset 0.25s var(--ease-decel);
}
.on .box {
  background: var(--primary);
  box-shadow: inset 0 0 0 2px var(--primary);
}
.on .box svg {
  stroke-dashoffset: 0;
}
</style>
