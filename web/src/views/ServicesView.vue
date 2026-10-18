<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import Icon from '@/components/Icon.vue'
import StatusDot, { type Tone } from '@/components/StatusDot.vue'
import LogViewer from '@/components/LogViewer.vue'
import { api, type Service } from '@/lib/api'
import { confirm, toast, toastError } from '@/lib/ui'

type Filter = 'all' | 'running' | 'failed' | 'inactive'
const route = useRoute()
const list = ref<Service[]>([])
const loading = ref(true)
const query = ref('')
const filter = ref<Filter>((['all', 'running', 'failed', 'inactive'] as const).find((f) => f === route.query.filter) ?? 'running')
const busy = ref<Record<string, boolean>>({})
const logsFor = ref<Service | null>(null)
const logsOpen = computed({ get: () => logsFor.value !== null, set: (v) => !v && (logsFor.value = null) })
const loadLogs = () => api.serviceLogs(logsFor.value!.name).then((r) => r.logs)

const matches = (s: Service, f: Filter) =>
  f === 'all' || (f === 'running' && s.active === 'active') || (f === 'failed' && s.active === 'failed') || (f === 'inactive' && s.active === 'inactive')
const counts = computed(() => Object.fromEntries((['all', 'running', 'failed', 'inactive'] as const).map((f) => [f, list.value.filter((s) => matches(s, f)).length])))
const shown = computed(() => {
  const q = query.value.trim().toLowerCase()
  return list.value.filter((s) => matches(s, filter.value) && (!q || s.name.toLowerCase().includes(q) || s.description.toLowerCase().includes(q)))
})

function tone(s: Service): Tone {
  if (s.active === 'failed') return 'bad'
  if (s.active === 'activating' || s.active === 'deactivating' || s.active === 'reloading') return 'warn'
  return s.active === 'active' ? 'ok' : 'idle'
}

async function load() {
  try {
    list.value = (await api.services()).services
  } catch (e) {
    toastError(e)
  } finally {
    loading.value = false
  }
}

async function act(s: Service, action: 'start' | 'stop' | 'restart') {
  if (action !== 'start') {
    const ok = await confirm({
      title: `${action === 'stop' ? 'Stop' : 'Restart'} ${s.name}?`,
      body: action === 'stop' ? 'It stays stopped until you start it again, or until the next boot if it is enabled.' : 'It will be unavailable for a moment.',
      action: action === 'stop' ? 'Stop' : 'Restart',
      danger: action === 'stop',
    })
    if (!ok) return
  }
  busy.value[s.name] = true
  try {
    await api.serviceAction(s.name, action)
    toast(`${s.name}: ${action} done`)
    await load()
  } catch (e) {
    toastError(e)
  } finally {
    busy.value[s.name] = false
  }
}

onMounted(load)
</script>

<template>
  <div class="page">
    <div class="page-title">
      <h1>Services</h1>
      <label class="field search">
        <Icon name="search" :size="20" />
        <input v-model="query" placeholder="Search services" aria-label="Search services" />
      </label>
    </div>

    <div class="filters" role="radiogroup" aria-label="Filter services">
      <button
        v-for="f in ['running', 'failed', 'inactive', 'all'] as const"
        :key="f"
        type="button"
        role="radio"
        class="filter st"
        :class="{ on: filter === f }"
        :aria-checked="filter === f"
        @click="filter = f"
      >
        <Icon v-if="filter === f" name="check" :size="18" />
        {{ f.charAt(0).toUpperCase() + f.slice(1) }} · {{ counts[f] }}
      </button>
    </div>

    <section class="card">
      <p v-if="loading" class="muted">Loading…</p>
      <p v-else-if="!shown.length" class="muted">No service matches.</p>
      <div v-for="s in shown" :key="s.name" class="row">
        <StatusDot :tone="tone(s)" :label="s.active" />
        <div class="grow">
          <div class="name">{{ s.name }}</div>
          <div class="muted small ellipsis">{{ s.description }}</div>
        </div>
        <span v-if="s.enabled" class="chip chip-outline">{{ s.enabled }}</span>
        <span class="muted small sub">{{ s.active }} · {{ s.sub }}</span>
        <span class="actions">
          <button v-if="s.active !== 'active'" class="icon-btn st" type="button" :aria-label="`Start ${s.name}`" :disabled="busy[s.name]" @click="act(s, 'start')">
            <Icon name="play" :size="20" />
          </button>
          <button v-else class="icon-btn st" type="button" :aria-label="`Restart ${s.name}`" :disabled="busy[s.name]" @click="act(s, 'restart')">
            <Icon name="restart" :size="20" />
          </button>
          <button class="icon-btn st" type="button" :aria-label="`Stop ${s.name}`" :disabled="busy[s.name] || s.active !== 'active'" @click="act(s, 'stop')">
            <Icon name="stop" :size="18" />
          </button>
          <button class="icon-btn st" type="button" :aria-label="`Logs of ${s.name}`" @click="logsFor = s">
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
  width: min(360px, 100%);
}
.filters {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
.filter {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  height: 36px;
  padding: 0 16px;
  border-radius: 10px;
  border: 1px solid var(--outline-v);
  background: transparent;
  color: var(--on-sv);
  font-size: 14px;
  font-weight: 500;
}
.filter.on {
  background: var(--secc);
  border-color: var(--secc);
  color: var(--on-secc);
}
.name {
  font-size: 16px;
  font-weight: 500;
}
.sub {
  width: 140px;
}
.actions {
  display: flex;
  gap: 4px;
}
</style>
