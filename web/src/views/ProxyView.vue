<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import Icon from '@/components/Icon.vue'
import ShapeLoader from '@/components/ShapeLoader.vue'
import { api, type ProxyReport, type ProxySite } from '@/lib/api'
import { confirm, toast, toastError } from '@/lib/ui'

const rep = ref<ProxyReport | null>(null)
const loading = ref(true)
const query = ref('')

const sites = computed(() => rep.value?.sites ?? [])
const engine = computed(() => (rep.value?.engine === 'caddy' ? 'Caddy' : rep.value?.engine === 'nginx' ? 'nginx' : ''))
// Names, not rows: a name served by two sites is still one domain.
const domains = computed(() => new Set(sites.value.flatMap((s) => s.domains)).size)

const shown = computed(() => {
  const q = query.value.trim().toLowerCase()
  if (!q) return sites.value
  return sites.value.filter(
    (s) => s.domains.some((d) => d.toLowerCase().includes(q)) || s.target.toLowerCase().includes(q) || s.file.toLowerCase().includes(q),
  )
})

// Two sites answering the same name is what makes a deploy fail with "domain already used".
const shared = computed(() => {
  const seen = new Map<string, number>()
  for (const s of sites.value) for (const d of s.domains) seen.set(d, (seen.get(d) ?? 0) + 1)
  return new Set([...seen].filter(([, n]) => n > 1).map(([d]) => d))
})

const url = (s: ProxySite) => `${s.scheme}://${s.domains[0]}`
const key = (s: ProxySite) => `${s.file}|${s.domains[0]}`
// What the server stores for the manual order: the app for a managed site, the file and
// its first domain otherwise.
const siteKey = (s: ProxySite) => (s.id ? `app:${s.id}` : `file:${s.file}|${s.domains[0] ?? ''}`)

const busy = ref('')
const dragFrom = ref<number | null>(null)
const dragTo = ref<number | null>(null)
// A filtered list would reorder the wrong rows: no dragging while searching.
const canDrag = computed(() => !query.value.trim())

async function togglePause(s: ProxySite) {
  busy.value = siteKey(s)
  const name = s.domains[0]
  try {
    rep.value = await api.proxyPause(s.id, !s.paused)
    toast(s.paused ? `${name} is back on` : `${name} paused, the app keeps running`)
  } catch (e) {
    toastError(e)
  } finally {
    busy.value = ''
  }
}

async function removeOrphan(s: ProxySite) {
  const ok = await confirm({
    title: `Remove ${s.domains[0] ?? s.file}?`,
    body: 'The panel wrote this site for an app that no longer exists. Removing it takes the domain out of the proxy; nothing else is touched.',
    action: 'Remove',
    danger: true,
  })
  if (!ok) return
  busy.value = siteKey(s)
  try {
    rep.value = await api.removeProxyOrphan(s.file)
    toast('Left-over site removed')
  } catch (e) {
    toastError(e)
  } finally {
    busy.value = ''
  }
}

async function saveOrder(list: ProxySite[]) {
  try {
    rep.value = await api.proxyOrder(list.map(siteKey))
  } catch (e) {
    toastError(e)
  }
}

async function move(from: number, to: number) {
  const list = [...(rep.value?.sites ?? [])]
  if (to < 0 || to >= list.length || from === to) return
  const [item] = list.splice(from, 1)
  if (!item) return
  list.splice(to, 0, item)
  if (rep.value) rep.value = { ...rep.value, sites: list }
  await saveOrder(list)
}

function dragStart(i: number) {
  dragFrom.value = i
}

async function drop(i: number) {
  const from = dragFrom.value
  dragFrom.value = dragTo.value = null
  if (from === null) return
  await move(from, i)
}

onMounted(async () => {
  try {
    const r = await api.proxy()
    // A server that sends null for a list must not blank the page: the view walks all three.
    r.sites = (r.sites ?? []).map((s) => ({ ...s, domains: s.domains ?? [] }))
    r.unreadable = r.unreadable ?? []
    rep.value = r
  } catch (e) {
    toastError(e)
  } finally {
    loading.value = false
  }
})
</script>

<template>
  <div class="page">
    <div class="page-title">
      <h1>Proxy</h1>
      <label class="field search">
        <Icon name="search" :size="20" />
        <input v-model="query" placeholder="Search domains" aria-label="Search domains" />
      </label>
    </div>

    <div v-if="loading" class="loading-block"><ShapeLoader label="Loading domains" /></div>

    <section v-else-if="!rep?.engine" class="card">
      <h2>No reverse proxy found</h2>
      <p class="muted small">
        Neither Caddy nor nginx answers on this machine, so no domain is published. Reinstall the agent with
        <code>--proxy caddy</code> or <code>--proxy nginx</code> to serve apps on a domain.
      </p>
    </section>

    <template v-else>
      <div class="head">
        <span class="chip chip-outline">{{ engine }}</span>
        <code class="muted small">{{ rep.root }}</code>
        <span class="muted small">{{ domains }} {{ domains === 1 ? 'domain' : 'domains' }}</span>
      </div>

      <p v-if="sites.length" class="hint muted small">
        Drag a row to set the order. Pausing a site takes its domain out of {{ engine }} and leaves the app running; the
        row stays here so it can be turned back on.
      </p>

      <section class="card">
        <p v-if="!shown.length" class="muted">{{ sites.length ? 'No domain matches.' : 'The proxy serves no site yet.' }}</p>
        <div
          v-for="(s, i) in shown"
          :key="key(s)"
          class="row site"
          :class="{ paused: s.paused, over: dragTo === i && dragFrom !== null && dragFrom !== i }"
          :draggable="canDrag"
          @dragstart="dragStart(i)"
          @dragover.prevent="dragTo = i"
          @dragleave="dragTo = null"
          @drop.prevent="drop(i)"
          @dragend="dragFrom = dragTo = null"
        >
          <button
            v-if="canDrag"
            class="grip st"
            type="button"
            :aria-label="`Reorder ${s.domains[0] ?? s.file}`"
            v-tip="'Drag, or use the up and down arrows'"
            @keydown.up.prevent="move(i, -1)"
            @keydown.down.prevent="move(i, 1)"
          >
            <span aria-hidden="true">⠿</span>
          </button>
          <div class="grow">
            <div class="names">
              <a :href="url(s)" target="_blank" rel="noopener" class="dom">
                {{ s.domains[0] }}<Icon name="external" :size="15" />
              </a>
              <span v-for="d in s.domains.slice(1)" :key="d" class="muted small">{{ d }}</span>
            </div>
            <div class="muted small ellipsis file">{{ s.file }}</div>
          </div>
          <span v-if="s.target" class="chip target">{{ s.target }}</span>
          <span v-else class="chip chip-outline">no upstream</span>
          <span v-if="s.vpnOnly" class="chip chip-outline">VPN only</span>
          <span v-if="s.scheme === 'http'" class="chip chip-outline warn">no HTTPS</span>
          <span v-if="s.domains.some((d) => shared.has(d))" class="chip chip-outline warn">also elsewhere</span>
          <span v-if="s.paused" class="chip chip-outline warn">paused</span>
          <RouterLink v-if="s.id" :to="`/apps/${s.id}`" class="chip">App {{ s.id }}</RouterLink>
          <span v-else-if="s.managed" class="chip chip-outline">left over</span>
          <span v-else class="chip chip-outline">by hand</span>
          <button
            v-if="s.id"
            class="icon-btn st"
            type="button"
            :aria-label="s.paused ? `Turn ${s.domains[0]} back on` : `Pause ${s.domains[0]}`"
            v-tip="s.paused ? 'Turn this URL back on' : 'Pause this URL'"
            :disabled="busy === siteKey(s)"
            @click="togglePause(s)"
          >
            <Icon :name="s.paused ? 'play' : 'stop'" :size="18" />
          </button>
          <button
            v-else-if="s.managed"
            class="icon-btn st"
            type="button"
            :aria-label="`Remove the left-over site ${s.domains[0] ?? s.file}`"
            v-tip="'Remove this left-over site'"
            :disabled="busy === siteKey(s)"
            @click="removeOrphan(s)"
          >
            <Icon name="trash" :size="18" />
          </button>
        </div>
      </section>

      <section v-if="rep.unreadable.length" class="card">
        <h2>Some files could not be read</h2>
        <p class="muted small">
          The agent runs as its own user, so a configuration file it cannot open is left out of this list.
        </p>
        <code v-for="f in rep.unreadable" :key="f" class="muted small line">{{ f }}</code>
      </section>
    </template>
  </div>
</template>

<style scoped>
.search {
  width: min(360px, 100%);
}
.head {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}
.names {
  display: flex;
  align-items: baseline;
  gap: 10px;
  flex-wrap: wrap;
}
.dom {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-size: 16px;
  font-weight: 500;
  color: var(--primary);
  text-decoration: none;
}
.dom:hover {
  text-decoration: underline;
}
.file {
  margin-top: 2px;
  font-family: var(--mono, ui-monospace, monospace);
  font-size: 12px;
}
.target {
  font-family: var(--mono, ui-monospace, monospace);
}
.warn {
  border-color: var(--warn);
  color: var(--warn);
}
.line {
  display: block;
  font-family: var(--mono, ui-monospace, monospace);
  margin-top: 4px;
}
.hint {
  margin: 0;
}
.site.paused {
  opacity: 0.62;
}
.site.over {
  box-shadow: inset 0 2px 0 var(--primary);
}
.grip {
  width: 28px;
  padding: 0;
  border: none;
  background: none;
  color: var(--on-surface-variant);
  font-size: 18px;
  line-height: 1;
  letter-spacing: -2px;
  cursor: grab;
  opacity: 0.7;
}
.grip:hover,
.grip:focus-visible {
  opacity: 1;
  color: var(--primary);
}
.site[draggable='true'] {
  cursor: default;
}
</style>
