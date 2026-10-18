<script setup lang="ts">
// Environment variables. Secrets never come back from the server: an existing secret
// shows as "unchanged" until the user types a new value.
import Icon from './Icon.vue'
import M3Checkbox from './M3Checkbox.vue'
import type { EnvVar } from '@/lib/api'

const vars = defineModel<EnvVar[]>({ required: true })

function add() {
  vars.value = [...vars.value, { key: '', value: '', secret: false }]
}
function remove(i: number) {
  vars.value = vars.value.filter((_, j) => j !== i)
}
function onValue(v: EnvVar, value: string) {
  v.value = value
  v.keep = false
}
// Paste KEY=value lines (a .env file) into the key field to add them all.
function onPaste(i: number, e: ClipboardEvent) {
  const text = e.clipboardData?.getData('text') ?? ''
  if (!text.includes('=') || !text.includes('\n')) return
  e.preventDefault()
  const parsed: EnvVar[] = []
  for (const line of text.split(/\r?\n/)) {
    const m = line.match(/^\s*(?:export\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.*)\s*$/)
    if (!m) continue
    let v = m[2]!
    if (/^(['"]).*\1$/.test(v)) v = v.slice(1, -1)
    parsed.push({ key: m[1]!, value: v, secret: /SECRET|TOKEN|PASSWORD|KEY|DATABASE_URL/i.test(m[1]!) })
  }
  const next = [...vars.value]
  next.splice(i, vars.value[i]?.key ? 0 : 1, ...parsed)
  vars.value = next
}
</script>

<template>
  <div class="env">
    <div v-for="(v, i) in vars" :key="i" class="env-row">
      <input
        v-model.trim="v.key"
        class="key"
        placeholder="NAME"
        aria-label="Variable name"
        spellcheck="false"
        @paste="onPaste(i, $event)"
      />
      <input
        :value="v.value"
        class="val"
        :type="v.secret ? 'password' : 'text'"
        :placeholder="v.keep ? 'unchanged' : 'value'"
        aria-label="Value"
        spellcheck="false"
        autocomplete="off"
        @input="onValue(v, ($event.target as HTMLInputElement).value)"
      />
      <M3Checkbox v-model="v.secret" v-tip="'Hidden in the panel once saved'" class="secret small" label="secret" />
      <button class="icon-btn st" type="button" :aria-label="`Remove ${v.key || 'variable'}`" @click="remove(i)">
        <Icon name="close" :size="18" />
      </button>
    </div>
    <p v-if="!vars.length" class="muted small">No variable. Tip: paste a whole .env file into a name field.</p>
    <button class="btn btn-tonal st add" type="button" @click="add"><Icon name="plus" :size="18" /> Add a variable</button>
  </div>
</template>

<style scoped>
.env {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.env-row {
  display: flex;
  align-items: center;
  gap: 8px;
}
.env-row input:not([type='checkbox']) {
  height: 40px;
  padding: 0 12px;
  border-radius: 10px;
  border: 1px solid var(--outline-v);
  background: var(--scc);
  color: var(--on-surface);
  font-family: var(--mono);
  font-size: 13px;
  min-width: 0;
}
.key {
  flex: 0 1 220px;
  color: var(--primary) !important;
}
.val {
  flex: 1;
}
.secret {
  display: flex;
  align-items: center;
  gap: 4px;
  white-space: nowrap;
}
.add {
  align-self: flex-start;
}
@media (max-width: 760px) {
  .env-row {
    flex-wrap: wrap;
  }
  .key {
    flex: 1 1 100%;
  }
}
</style>
