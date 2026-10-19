<script setup lang="ts" generic="T extends string">
// M3 exposed dropdown menu. Replaces <select>, whose popup is drawn by the browser.
// Keyboard: arrows move, Enter or Space picks, Escape closes, letters jump.
import { computed, nextTick, onBeforeUnmount, ref } from 'vue'
import Icon from './Icon.vue'

export interface Option<V> {
  value: V
  label: string
  hint?: string
}

const model = defineModel<T>({ required: true })
const props = defineProps<{ options: Option<T>[]; label: string; disabled?: boolean }>()

const open = ref(false)
const active = ref(0)
const root = ref<HTMLElement>()
const list = ref<HTMLElement>()
const id = `sel-${Math.random().toString(36).slice(2, 8)}`
const current = computed(() => props.options.find((o) => o.value === model.value))

async function show() {
  if (props.disabled) return
  open.value = true
  active.value = Math.max(0, props.options.findIndex((o) => o.value === model.value))
  document.addEventListener('pointerdown', outside, true)
  await nextTick()
  list.value?.querySelector<HTMLElement>(`[data-i="${active.value}"]`)?.scrollIntoView({ block: 'nearest' })
}
function hide() {
  open.value = false
  document.removeEventListener('pointerdown', outside, true)
}
function outside(e: Event) {
  if (!root.value?.contains(e.target as Node)) hide()
}
function pick(i: number) {
  const o = props.options[i]
  if (o) model.value = o.value
  hide()
}
function move(d: number) {
  active.value = (active.value + d + props.options.length) % props.options.length
  nextTick(() => list.value?.querySelector<HTMLElement>(`[data-i="${active.value}"]`)?.scrollIntoView({ block: 'nearest' }))
}
function onKey(e: KeyboardEvent) {
  if (props.disabled) return
  if (!open.value) {
    if (['ArrowDown', 'ArrowUp', 'Enter', ' '].includes(e.key)) {
      e.preventDefault()
      show()
    }
    return
  }
  if (e.key === 'ArrowDown') move(1)
  else if (e.key === 'ArrowUp') move(-1)
  else if (e.key === 'Home') active.value = 0
  else if (e.key === 'End') active.value = props.options.length - 1
  else if (e.key === 'Enter' || e.key === ' ') pick(active.value)
  else if (e.key === 'Escape' || e.key === 'Tab') return hide()
  else if (e.key.length === 1) {
    const i = props.options.findIndex((o, j) => j > active.value && o.label.toLowerCase().startsWith(e.key.toLowerCase()))
    const k = i >= 0 ? i : props.options.findIndex((o) => o.label.toLowerCase().startsWith(e.key.toLowerCase()))
    if (k >= 0) active.value = k
    return
  } else return
  e.preventDefault()
}
onBeforeUnmount(() => document.removeEventListener('pointerdown', outside, true))
</script>

<template>
  <div ref="root" class="select" :class="{ open, disabled }">
    <button
      type="button"
      class="field-btn st"
      role="combobox"
      :aria-expanded="open"
      :aria-controls="id"
      :aria-activedescendant="open ? `${id}-${active}` : undefined"
      :aria-label="label"
      :disabled="disabled"
      @click="open ? hide() : show()"
      @keydown="onKey"
    >
      <span class="lbl">{{ label }}</span>
      <span class="val">{{ current?.label ?? '' }}</span>
      <Icon name="chevron" :size="20" class="chev" />
    </button>
    <Transition name="menu">
      <ul v-if="open" :id="id" ref="list" class="menu" role="listbox" :aria-label="label">
        <li
          v-for="(o, i) in options"
          :id="`${id}-${i}`"
          :key="o.value"
          :data-i="i"
          role="option"
          class="opt"
          :class="{ active: i === active, sel: o.value === model }"
          :aria-selected="o.value === model"
          @pointerenter="active = i"
          @click="pick(i)"
        >
          <span class="grow">
            <span class="opt-label">{{ o.label }}</span>
            <span v-if="o.hint" class="opt-hint">{{ o.hint }}</span>
          </span>
          <Icon v-if="o.value === model" name="check" :size="18" />
        </li>
      </ul>
    </Transition>
  </div>
</template>

<style scoped>
.select {
  position: relative;
  display: flex;
}
/* Same filled field as the text fields beside it, filling its grid row */
.field-btn {
  flex: 1;
  display: grid;
  grid-template-columns: 1fr auto;
  align-content: center;
  align-items: center;
  gap: 4px 8px;
  width: 100%;
  min-height: 56px;
  padding: 10px 12px 8px 16px;
  border: none;
  border-bottom: 1px solid var(--on-sv);
  border-radius: 12px 12px 4px 4px;
  background: var(--schh);
  color: var(--on-surface);
  text-align: left;
  transition: border-color 0.2s var(--ease);
}
.open .field-btn,
.field-btn:focus-visible {
  border-bottom-color: var(--primary);
  box-shadow: inset 0 -1px 0 var(--primary);
  outline: none;
}
.lbl {
  grid-column: 1;
  font-size: 12px;
  font-weight: 500;
  color: var(--primary);
}
.val {
  grid-column: 1;
  font-size: 16px;
}
.chev {
  grid-column: 2;
  grid-row: 1 / span 2;
  color: var(--on-sv);
  transition: rotate 0.25s var(--ease);
}
.open .chev {
  rotate: 180deg;
}
.menu {
  position: absolute;
  z-index: 30;
  left: 0;
  right: 0;
  top: calc(100% + 4px);
  max-height: 320px;
  overflow-y: auto;
  margin: 0;
  padding: 8px 0;
  list-style: none;
  border-radius: 16px;
  background: var(--schh);
  box-shadow:
    0 2px 6px rgba(0, 0, 0, 0.25),
    0 8px 24px rgba(0, 0, 0, 0.3);
  transform-origin: top center;
}
.opt {
  display: flex;
  align-items: center;
  gap: 12px;
  min-height: 48px;
  padding: 6px 16px;
  cursor: pointer;
  font-size: 15px;
}
.opt.active {
  background: color-mix(in srgb, var(--on-surface) 8%, transparent);
}
.opt.sel {
  color: var(--primary);
}
.opt-label,
.opt-hint {
  display: block;
}
.opt-hint {
  font-size: 12px;
  color: var(--on-sv);
}
.menu-enter-active {
  transition:
    opacity 0.15s,
    scale 0.25s var(--ease-decel);
}
.menu-leave-active {
  transition: opacity 0.1s;
}
.menu-enter-from {
  opacity: 0;
  scale: 1 0.8;
}
.menu-leave-to {
  opacity: 0;
}
.disabled {
  opacity: 0.5;
}
</style>
