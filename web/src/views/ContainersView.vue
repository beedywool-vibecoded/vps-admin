<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import Icon from '@/components/Icon.vue'
import M3Shape from '@/components/M3Shape.vue'
import StatusDot, { type Tone } from '@/components/StatusDot.vue'
import LogViewer from '@/components/LogViewer.vue'
import { api, type Container, type ContainerStats } from '@/lib/api'
import { bytes, image } from '@/lib/format'
import { confirm, toast, toastError } from '@/lib/ui'
import type { ShapeKind } from '@/lib/shapes'

const list = ref<Container[]>([])
const stats = ref<Record<string, ContainerStats>>({})
const loading = ref(true)
const error = ref('')
const query = ref('')
const busy = ref<Record<string, boolean>>({})
const logsFor = ref<Container | null>(null)
const logsOpen = computed({ get: () => logsFor.value !== null, set: (v) => !v && (logsFor.value = null) })
const loadLogs = () => api.containerLogs(logsFor.value!.id).then((r) => r.logs)

const SHAPES: ShapeKind[] = ['cookie', 'clover', 'sunny', 'flower']
const groups = computed(() => {
  const q = query.value.trim().toLowerCase()
  const map = new Map<string, Container[]>()
  for (const c of list.value) {
    if (q && !c.name.toLowerCase().includes(q) && !c.image.toLowerCase().includes(q) && !(c.project ?? '').includes(q)) continue
    const key = c.project || 'Standalone'
    map.set(key, [...(map.get(key) ?? []), c])
  }
  return [...map.entries()].sort(([a], [b]) => (a === 'Standalone' ? 1 : b === 'Standalone' ? -1 : a.localeCompare(b)))
})

function tone(c: Container): Tone {
  if (c.health === 'unhealthy' || c.state === 'restarting' || c.state === 'dead') return 'bad'
  if (c.state !== 'running') return 'idle'
  return c.health === 'starting' ? 'warn' : 'ok'
}
const ports = (c: Container) =>
  c.ports
    .filter((p) => p.publicPort)
    .map((p) => `${p.ip && p.ip !== '0.0.0.0' && p.ip !== '::' ? p.ip + ':' : ''}${p.publicPort}→${p.privatePort}`)
    .join(', ')

async function load() {
  try {
    list.value = (await api.containers()).containers
    error.value = ''
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    loading.value = false
  }
}
async function loadStats() {
  try {
    const s = await api.containerStats()
    stats.value = Object.fromEntries(s.stats.map((x) => [x.id, x]))
  } catch {
    // stats are a bonus; the list still works without them
  }
}

async function act(c: Container, action: 'start' | 'stop' | 'restart') {
  if (action === 'stop') {
    const ok = await confirm({ title: `Stop ${c.name}?`, body: 'Its restart policy may start it again at the next reboot.', action: 'Stop', danger: true })
    if (!ok) return
  }
  busy.value[c.id] = true
  try {
    await api.containerAction(c.id, action)
    toast(`${c.name}: ${action} done`)
    await load()
  } catch (e) {
    toastError(e)
  } finally {
    busy.value[c.id] = false
  }
}

let timer: ReturnType<typeof setInterval> | undefined
onMounted(async () => {
  await load()
  loadStats()
  timer = setInterval(() => (load(), loadStats()), 15000)
})
onBeforeUnmount(() => clearInterval(timer))
</script>

<template>
  <div class="page">
    <div class="page-title">
      <h1>Containers</h1>
      <label class="field search">
        <Icon name="search" :size="20" />
        <input v-model="query" placeholder="Search containers, images, projects" aria-label="Search containers" />
      </label>
    </div>

    <p v-if="loading" class="muted">Loading…</p>
    <section v-else-if="error" class="card">
      <h2>Docker is not reachable</h2>
      <p class="muted">{{ error }}. Check that Docker runs and that the vps-admin user is in the docker group.</p>
    </section>
    <p v-else-if="!groups.length" class="muted">No container.</p>

    <section v-for="([name, items], gi) in groups" :key="name" class="card">
      <div class="card-head">
        <M3Shape :kind="SHAPES[gi % SHAPES.length]!" :size="28" :fill="gi % 2 ? 'var(--secc)' : 'var(--pc)'" />
        <h2 class="gname">{{ name }}<code v-if="items[0]?.workDir" class="path">{{ items[0].workDir }}</code></h2>
        <span class="chip">{{ items.length }}</span>
      </div>
      <div v-for="c in items" :key="c.id" class="row">
        <StatusDot :tone="tone(c)" :label="c.health ?? c.state" />
        <div class="grow">
          <div class="name">{{ c.name }}</div>
          <div class="muted small ellipsis">{{ image(c.image) }}<template v-if="ports(c)"> · {{ ports(c) }}</template></div>
        </div>
        <span v-if="c.exposed" class="chip chip-outline" v-tip="'Docker bypasses ufw for ports published on every interface'">public</span>
        <span class="muted small stat">
          <template v-if="stats[c.id]">{{ stats[c.id]!.cpu.toFixed(1) }}% · {{ bytes(stats[c.id]!.memUsed) }}</template>
          <template v-else>{{ c.status }}</template>
        </span>
        <span class="actions">
          <button v-if="c.state !== 'running'" class="icon-btn st" type="button" :aria-label="`Start ${c.name}`" :disabled="busy[c.id]" @click="act(c, 'start')">
            <Icon name="play" :size="20" />
          </button>
          <button v-else class="icon-btn st" type="button" :aria-label="`Restart ${c.name}`" :disabled="busy[c.id]" @click="act(c, 'restart')">
            <Icon name="restart" :size="20" />
          </button>
          <button class="icon-btn st" type="button" :aria-label="`Stop ${c.name}`" :disabled="busy[c.id] || c.state !== 'running'" @click="act(c, 'stop')">
            <Icon name="stop" :size="18" />
          </button>
          <button class="icon-btn st" type="button" :aria-label="`Logs of ${c.name}`" @click="logsFor = c">
            <Icon name="logs" :size="20" />
          </button>
        </span>
      </div>
    </section>

    <LogViewer
      v-if="logsFor"
      v-model="logsOpen"
      :title="`${logsFor.name} logs`"
      :load="loadLogs"
    />
  </div>
</template>

<style scoped>
.search {
  width: min(380px, 100%);
}
.gname {
  display: flex;
  align-items: baseline;
  flex-wrap: wrap;
  gap: 4px 12px;
  min-width: 0;
}
.path {
  font-size: 12px;
  font-weight: 400;
  color: var(--on-sv);
  word-break: break-all;
}
.name {
  font-size: 16px;
  font-weight: 500;
}
.stat {
  width: 150px;
  text-align: right;
}
.actions {
  display: flex;
  gap: 4px;
}
@media (max-width: 760px) {
  .stat {
    width: auto;
    text-align: left;
  }
}
</style>
