<script setup lang="ts">
// Log dialog for a service or a container, with refresh and follow.
import { nextTick, onBeforeUnmount, ref, watch } from 'vue'
import M3Dialog from './M3Dialog.vue'
import M3Switch from './M3Switch.vue'
import ShapeLoader from './ShapeLoader.vue'
import { toastError } from '@/lib/ui'

const open = defineModel<boolean>({ required: true })
const props = defineProps<{ title: string; load: () => Promise<string> }>()

const text = ref('')
const loading = ref(false)
const follow = ref(true)
const pre = ref<HTMLElement>()
let timer: ReturnType<typeof setInterval> | undefined

async function refresh() {
  loading.value = true
  try {
    const atBottom = !pre.value || pre.value.scrollHeight - pre.value.scrollTop - pre.value.clientHeight < 40
    text.value = (await props.load()) || 'No output yet.'
    if (atBottom) {
      await nextTick()
      pre.value?.scrollTo({ top: pre.value.scrollHeight })
    }
  } catch (e) {
    toastError(e)
  } finally {
    loading.value = false
  }
}

watch(
  [open, follow],
  () => {
    clearInterval(timer)
    if (!open.value) return
    if (!text.value) refresh()
    if (follow.value) timer = setInterval(refresh, 4000)
  },
  { immediate: true },
)
watch(open, (v) => !v && (text.value = ''))
onBeforeUnmount(() => clearInterval(timer))
</script>

<template>
  <M3Dialog v-model="open" :title="title" wide>
    <template #header>
      <label class="follow small muted">Follow <M3Switch v-model="follow" label="Follow new lines" /></label>
      <button class="btn btn-tonal st" type="button" :disabled="loading" @click="refresh">Refresh</button>
    </template>
    <pre v-if="text" ref="pre" class="logs">{{ text }}</pre>
    <div v-else class="logs logs-empty" aria-busy="true"><ShapeLoader :label="`Loading ${title}`" /></div>
  </M3Dialog>
</template>

<style scoped>
.follow {
  display: flex;
  align-items: center;
  gap: 10px;
}
.logs-empty {
  display: grid;
  place-items: center;
}
.logs {
  margin: 0;
  min-height: 300px;
  max-height: calc(100vh - 200px);
  overflow: auto;
  padding: 16px;
  border-radius: 16px;
  background: var(--scc);
  border: 1px solid var(--sch);
  font-size: 12px;
  line-height: 1.6;
  white-space: pre-wrap;
  word-break: break-all;
}
</style>
