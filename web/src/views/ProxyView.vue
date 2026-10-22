<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import Icon from '@/components/Icon.vue'
import { api, type ProxyReport, type ProxySite } from '@/lib/api'
import { toastError } from '@/lib/ui'

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

    <p v-if="loading" class="muted">Loading…</p>

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

      <section class="card">
        <p v-if="!shown.length" class="muted">{{ sites.length ? 'No domain matches.' : 'The proxy serves no site yet.' }}</p>
        <div v-for="s in shown" :key="key(s)" class="row">
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
          <RouterLink v-if="s.id" :to="`/apps/${s.id}`" class="chip">App {{ s.id }}</RouterLink>
          <span v-else-if="s.managed" class="chip chip-outline">left over</span>
          <span v-else class="chip chip-outline">by hand</span>
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
</style>
