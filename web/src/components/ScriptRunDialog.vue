<script setup lang="ts">
// Runs a linked script and streams its output.
import { nextTick, ref, watch } from 'vue'
import M3Dialog from './M3Dialog.vue'
import Icon from './Icon.vue'
import { api, streamScript, type Script } from '@/lib/api'
import { toastError } from '@/lib/ui'

const open = defineModel<boolean>({ required: true })
const props = defineProps<{ script: Script | null }>()
const emit = defineEmits<{ done: [] }>()
const args = ref('')
const output = ref('')
const state = ref<'ready' | 'running' | 'done'>('ready')
const result = ref<{ exitCode?: number; error?: string }>({})
const pre = ref<HTMLElement>()
let close: (() => void) | undefined

watch(open, (v) => {
  if (v) {
    args.value = ''
    output.value = ''
    result.value = {}
    state.value = props.script?.running ? 'running' : 'ready'
    if (props.script?.running) follow()
  } else {
    close?.()
  }
})

function follow() {
  close = streamScript(props.script!.name, {
    output: async (c) => {
      output.value += c
      await nextTick()
      pre.value?.scrollTo({ top: pre.value.scrollHeight })
    },
    end: (r) => {
      result.value = r
      state.value = 'done'
      emit('done')
    },
  })
}

async function run() {
  try {
    await api.runScript(props.script!.name, args.value.trim())
    state.value = 'running'
    follow()
  } catch (e) {
    toastError(e)
  }
}
</script>

<template>
  <M3Dialog v-model="open" :title="script ? (script.title ? `${script.title} (${script.name})` : script.name) : ''" wide>
    <template #header>
      <span v-if="state === 'running'" class="chip">running</span>
      <span v-else-if="state === 'done'" class="chip" :class="result.exitCode === 0 && !result.error ? 'ok' : 'bad'">exit {{ result.exitCode }}</span>
    </template>
    <p v-if="script?.description" class="muted small" style="margin: 0">{{ script.description }}</p>
    <div v-if="state === 'ready'" class="run-row">
      <label class="field grow"><input v-model="args" placeholder="Arguments (optional)" aria-label="Arguments" spellcheck="false" @keydown.enter="run" /></label>
      <button class="btn btn-filled st" type="button" @click="run"><Icon name="play" :size="18" /> Run</button>
    </div>
    <pre v-else ref="pre" class="out">{{ output || 'Waiting for output…' }}<template v-if="result.error">{{ '\n' + result.error }}</template></pre>
    <template #actions>
      <button v-if="state === 'running'" class="btn btn-danger st" type="button" @click="api.stopScript(script!.name)">Stop</button>
      <button class="btn btn-text st" type="button" @click="open = false">Close</button>
    </template>
  </M3Dialog>
</template>

<style scoped>
.run-row {
  display: flex;
  gap: 8px;
  align-items: center;
}
.run-row input {
  font-family: var(--mono);
  font-size: 13px;
}
.out {
  margin: 0;
  min-height: 260px;
  max-height: calc(100vh - 260px);
  overflow: auto;
  padding: 14px 16px;
  border-radius: 16px;
  background: var(--scc);
  border: 1px solid var(--sch);
  font-size: 12px;
  line-height: 1.6;
  white-space: pre-wrap;
  word-break: break-all;
}
.ok {
  background: var(--ok);
  color: var(--on-ok);
}
.bad {
  background: var(--bad);
  color: var(--on-bad);
}
</style>
