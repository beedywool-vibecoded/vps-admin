<script setup lang="ts">
// Pulls or rebuilds the compose service of one container, with the output live.
import { nextTick, ref, watch } from 'vue'
import M3Dialog from './M3Dialog.vue'
import Icon from './Icon.vue'
import { rebuildContainer, type Container } from '@/lib/api'
import { toastError } from '@/lib/ui'

const open = defineModel<boolean>({ required: true })
const props = defineProps<{ container: Container }>()
const emit = defineEmits<{ done: [] }>()
const output = ref('')
const state = ref<'ready' | 'running' | 'ok' | 'failed'>('ready')
const pre = ref<HTMLElement>()

watch(open, (v) => {
  if (v && state.value !== 'running') {
    output.value = ''
    state.value = 'ready'
  }
})

async function run() {
  state.value = 'running'
  try {
    const ok = await rebuildContainer(props.container.id, async (t) => {
      output.value += t
      await nextTick()
      pre.value?.scrollTo({ top: pre.value.scrollHeight })
    })
    state.value = ok ? 'ok' : 'failed'
    emit('done')
  } catch (e) {
    state.value = 'ready'
    toastError(e)
  }
}
</script>

<template>
  <M3Dialog v-model="open" :title="`Rebuild ${container.name}`" wide>
    <template #header>
      <span v-if="state === 'running'" class="chip">running</span>
      <span v-else-if="state === 'ok'" class="chip ok">done</span>
      <span v-else-if="state === 'failed'" class="chip bad">failed</span>
    </template>
    <p v-if="state === 'ready'" class="muted" style="margin: 0">
      Pulls a newer image of <code>{{ container.service }}</code> (or rebuilds it if it has a build section), then recreates this
      container alone from <code class="plain">{{ container.workDir }}</code>. The other containers of {{ container.project }} keep running.
    </p>
    <pre v-else ref="pre" class="out">{{ output || 'Starting…' }}</pre>
    <template #actions>
      <button class="btn btn-text st" type="button" @click="open = false">{{ state === 'ready' ? 'Cancel' : 'Close' }}</button>
      <button v-if="state === 'ready'" class="btn btn-filled st" type="button" @click="run"><Icon name="restart" :size="18" /> Rebuild</button>
    </template>
  </M3Dialog>
</template>

<style scoped>
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
