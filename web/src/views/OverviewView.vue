<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import MetricCard from '@/components/MetricCard.vue'
import M3Shape from '@/components/M3Shape.vue'
import Icon from '@/components/Icon.vue'
import StatusDot, { type Tone } from '@/components/StatusDot.vue'
import UsageChart from '@/components/UsageChart.vue'
import { api, type Container, type Point, type Security, type Service, type UpdateStatus } from '@/lib/api'
import { session } from '@/lib/session'
import { bytes, image, pct, rate } from '@/lib/format'
import { toastError } from '@/lib/ui'

const span = ref<'1h' | '24h'>('1h')
const points = ref<Point[]>([])
const security = ref<Security | null>(null)
const update = ref<UpdateStatus | null>(null)
const containers = ref<Container[] | null>(null)
const dockerError = ref('')
const services = ref<Service[]>([])

const snap = computed(() => session.snapshot)
const memPct = computed(() => (snap.value?.memTotal ? (100 * snap.value.memUsed) / snap.value.memTotal : 0))
const rootDisk = computed(() => {
  const d = snap.value?.disks ?? []
  return d.find((x) => x.mount === '/') ?? d[0]
})
const diskPct = computed(() => (rootDisk.value ? (100 * rootDisk.value.used) / rootDisk.value.total : 0))
const running = computed(() => containers.value?.filter((c) => c.state === 'running').length ?? 0)
const healthy = computed(() => containers.value?.filter((c) => c.health === 'healthy').length ?? 0)

const failedServices = computed(() => services.value.filter((s) => s.active === 'failed'))
const badContainers = computed(
  () => containers.value?.filter((c) => c.health === 'unhealthy' || c.state === 'restarting' || c.state === 'dead') ?? [],
)

// The banner shows the most urgent thing to look at, if any.
const alert = computed(() => {
  if (failedServices.value.length)
    return { text: `${failedServices.value.length} service${failedServices.value.length > 1 ? 's' : ''} failed: ${failedServices.value.map((s) => s.name).join(', ')}`, to: '/services?filter=failed' }
  if (badContainers.value.length)
    return { text: `Container trouble: ${badContainers.value.map((c) => `${c.name} (${c.health ?? c.state})`).join(', ')}`, to: '/containers' }
  const fail = security.value?.checks.find((c) => c.status === 'fail')
  if (fail) return { text: `${fail.title}: ${fail.detail}`, to: '/security' }
  return null
})

const toFix = computed(() => security.value?.checks.filter((c) => c.status === 'warn' || c.status === 'fail') ?? [])
const securityItems = computed(() => {
  const order = { fail: 0, warn: 1, ok: 2, unknown: 3 }
  return [...(security.value?.checks ?? [])].sort((a, b) => order[a.status] - order[b.status]).slice(0, 8)
})
const packages = computed(() => security.value?.checks.find((c) => c.id === 'updates'))

interface Group {
  name: string
  items: Container[]
}
const groups = computed<Group[]>(() => {
  const map = new Map<string, Container[]>()
  for (const c of containers.value ?? []) {
    const key = c.project || 'Standalone'
    map.set(key, [...(map.get(key) ?? []), c])
  }
  return [...map.entries()]
    .map(([name, items]) => ({ name, items }))
    .sort((a, b) => (a.name === 'Standalone' ? 1 : b.name === 'Standalone' ? -1 : a.name.localeCompare(b.name)))
})

function tone(c: Container): Tone {
  if (c.health === 'unhealthy' || c.state === 'restarting' || c.state === 'dead') return 'bad'
  if (c.state !== 'running') return 'idle'
  return c.health === 'starting' ? 'warn' : 'ok'
}
function groupTone(g: Group): Tone {
  const t = g.items.map(tone)
  return t.includes('bad') ? 'bad' : t.includes('warn') ? 'warn' : t.every((x) => x === 'idle') ? 'idle' : 'ok'
}

async function loadHistory() {
  try {
    points.value = (await api.history(span.value)).points
  } catch (e) {
    toastError(e)
  }
}

async function load() {
  const results = await Promise.allSettled([
    api.security().then((s) => (security.value = s)),
    api.update().then((u) => (update.value = u)),
    api.services().then((s) => (services.value = s.services)),
    api.containers().then((c) => (containers.value = c.containers)),
  ])
  const docker = results[3]
  dockerError.value = docker.status === 'rejected' ? String((docker.reason as Error).message) : ''
}

watch(span, loadHistory)
// New samples arrive with every snapshot; append them to the 1h view.
watch(
  () => session.snapshot?.time,
  () => {
    const s = session.snapshot
    if (!s || span.value !== '1h') return
    const t = Math.floor(new Date(s.time).getTime() / 1000)
    const lastT = points.value[points.value.length - 1]?.t ?? 0
    if (t <= lastT) return
    points.value = [
      ...points.value.slice(-719),
      { t, cpu: s.cpu, mem: s.memTotal ? (100 * s.memUsed) / s.memTotal : 0, rx: s.netRx, tx: s.netTx, load: s.load[0] },
    ]
  },
)

let timer: ReturnType<typeof setInterval> | undefined
onMounted(() => {
  loadHistory()
  load()
  timer = setInterval(load, 30000)
})
onBeforeUnmount(() => clearInterval(timer))
</script>

<template>
  <div class="page">
    <div v-if="alert" class="banner" role="status">
      <Icon name="alert" :size="24" />
      <span class="grow">{{ alert.text }}</span>
      <RouterLink :to="alert.to" class="btn st banner-btn">View</RouterLink>
    </div>

    <section class="metrics" aria-label="Resources">
      <MetricCard
        :label="`CPU · ${session.host?.cores ?? '?'} cores`"
        :value="snap ? pct(snap.cpu) : '…'"
        :percent="snap?.cpu ?? 0"
        :sub="snap ? `Load ${snap.load.map((l) => l.toFixed(2)).join(' · ')}` : ''"
        shape="cookie"
        tint="var(--pc)"
      />
      <MetricCard
        label="Memory"
        :value="snap ? pct(memPct) : '…'"
        :percent="memPct"
        :sub="snap ? `${bytes(snap.memUsed)} of ${bytes(snap.memTotal)}` : ''"
        shape="clover"
        tint="var(--secc)"
      />
      <MetricCard
        :label="`Disk ${rootDisk?.mount ?? ''}`"
        :value="rootDisk ? pct(diskPct) : '…'"
        :percent="diskPct"
        :sub="rootDisk ? `${bytes(rootDisk.used)} of ${bytes(rootDisk.total)}` : ''"
        shape="sunny"
        tint="var(--tc)"
      />
      <MetricCard
        label="Containers"
        :value="containers ? String(running) : dockerError ? 'n/a' : '…'"
        :percent="containers?.length ? (100 * running) / containers.length : 0"
        :sub="dockerError ? 'Docker not reachable' : `${running} running · ${healthy} healthy`"
        shape="flower"
        tint="var(--pc)"
      />
    </section>

    <section class="card">
      <div class="card-head">
        <h2>Usage</h2>
        <span v-if="snap" class="chip chip-outline">Network ↓ {{ rate(snap.netRx) }} · ↑ {{ rate(snap.netTx) }}</span>
        <div class="seg" role="radiogroup" aria-label="Time range">
          <button
            v-for="s in ['1h', '24h'] as const"
            :key="s"
            type="button"
            role="radio"
            class="st"
            :aria-checked="span === s"
            :class="{ on: span === s }"
            @click="span = s"
          >
            {{ s === '1h' ? 'Last hour' : 'Last day' }}
          </button>
        </div>
      </div>
      <UsageChart :points="points" :span="span" />
    </section>

    <section class="two">
      <div class="card stack">
        <div class="sec-head">
          <div class="score">
            <M3Shape kind="sunny" :size="84" fill="var(--tc)" />
            <span>{{ security ? Math.round(security.score / 10) : '…' }}/10</span>
          </div>
          <div class="grow">
            <h2>Security</h2>
            <span class="muted small">{{ security ? (toFix.length ? `${toFix.length} point${toFix.length > 1 ? 's' : ''} to fix` : 'Nothing to fix') : 'Checking…' }}</span>
          </div>
          <RouterLink to="/security" class="btn btn-filled st">Details</RouterLink>
        </div>
        <ul class="checks">
          <li v-for="c in securityItems" :key="c.id">
            <span class="mark" :class="c.status">{{ c.status === 'ok' ? '✓' : c.status === 'unknown' ? '?' : '!' }}</span>
            <span class="ellipsis">{{ c.title }}</span>
          </li>
        </ul>
      </div>

      <div class="card stack">
        <div class="card-head" style="margin: 0">
          <h2>Updates</h2>
          <span v-if="update" class="chip">Auto-update {{ update.auto ? 'on' : 'off' }}{{ update.prerelease ? ' · pre-releases' : '' }}</span>
        </div>
        <div class="tile">
          <M3Shape kind="cookie" :size="40" fill="var(--pc)" />
          <div class="grow">
            <div class="tile-title">VPS Admin agent {{ update?.current ?? session.version }}</div>
            <div class="muted small">
              {{ update?.available ? `${update.latest?.tag} is available` : update?.checkedAt ? 'Up to date, signed builds only' : 'Not checked yet' }}
            </div>
          </div>
          <RouterLink to="/updates" class="btn st" :class="update?.available ? 'btn-filled' : 'btn-tonal'">
            {{ update?.available ? 'Install' : 'Open' }}
          </RouterLink>
        </div>
        <div class="tile">
          <M3Shape kind="clover" :size="40" fill="var(--tc)" />
          <div class="grow">
            <div class="tile-title">System packages</div>
            <div class="muted small">{{ packages?.detail ?? 'Checking…' }}</div>
          </div>
        </div>
      </div>
    </section>

    <section v-if="failedServices.length" class="card">
      <div class="card-head">
        <M3Shape kind="clover" :size="28" fill="var(--ec)" />
        <h2>Failed services</h2>
        <RouterLink to="/services?filter=failed" class="btn btn-text st">Open services</RouterLink>
      </div>
      <div v-for="s in failedServices" :key="s.name" class="row">
        <StatusDot tone="bad" label="Failed" />
        <div class="grow">
          <div class="name">{{ s.name }}</div>
          <div class="muted small ellipsis">{{ s.description }}</div>
        </div>
      </div>
    </section>

    <section v-for="g in groups" :key="g.name" class="card" :aria-label="g.name">
      <div class="card-head">
        <StatusDot :tone="groupTone(g)" :label="groupTone(g) === 'ok' ? 'Running' : 'Needs attention'" />
        <h2>{{ g.name }}</h2>
        <span class="chip">{{ g.items.length }}</span>
        <RouterLink to="/containers" class="btn btn-text st">Manage</RouterLink>
      </div>
      <div v-for="c in g.items" :key="c.id" class="row">
        <StatusDot :tone="tone(c)" :label="c.health ?? c.state" />
        <div class="grow">
          <div class="name">{{ c.name }}</div>
          <div class="muted small ellipsis">{{ image(c.image) }}</div>
        </div>
        <span v-if="c.exposed" class="chip chip-outline" title="Published on every interface">public port</span>
        <span class="muted small status">{{ c.status }}</span>
      </div>
    </section>

    <p v-if="dockerError" class="muted small">Docker: {{ dockerError }}</p>
  </div>
</template>

<style scoped>
.banner {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 16px;
  padding: 16px 20px;
  border-radius: 20px;
  background: var(--ec);
  color: var(--on-ec);
}
.banner-btn {
  background: var(--error);
  color: var(--on-error);
}
.metrics {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(230px, 1fr));
  gap: 12px;
}
.two {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(min(420px, 100%), 1fr));
  gap: 12px;
}
.stack {
  display: flex;
  flex-direction: column;
  gap: 14px;
}
.sec-head {
  display: flex;
  align-items: center;
  gap: 16px;
}
.sec-head h2 {
  font-size: 22px;
}
.score {
  position: relative;
  width: 84px;
  height: 84px;
  display: grid;
  place-items: center;
  flex-shrink: 0;
}
.score svg {
  position: absolute;
  inset: 0;
}
.score span {
  position: relative;
  font-size: 24px;
  font-weight: 600;
  color: var(--on-tc);
}
.checks {
  list-style: none;
  margin: 0;
  padding: 0;
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(190px, 1fr));
  gap: 8px;
}
.checks li {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 12px;
  border-radius: 14px;
  background: var(--sch);
  font-size: 14px;
  min-width: 0;
}
.mark {
  width: 22px;
  height: 22px;
  border-radius: 11px;
  display: grid;
  place-items: center;
  flex-shrink: 0;
  font-size: 13px;
  font-weight: 700;
}
.mark.ok {
  background: var(--ok);
  color: var(--on-ok);
}
.mark.warn {
  background: var(--warn);
  color: var(--on-warn);
}
.mark.fail {
  background: var(--bad);
  color: var(--on-bad);
}
.mark.unknown {
  background: var(--schh);
  color: var(--on-sv);
}
.tile {
  display: flex;
  align-items: center;
  gap: 14px;
  padding: 14px 16px;
  border-radius: 18px;
  background: var(--sch);
}
.tile-title,
.name {
  font-size: 16px;
  font-weight: 500;
}
.status {
  width: 160px;
  text-align: right;
}
.seg {
  display: inline-flex;
  border: 1px solid var(--outline);
  border-radius: 20px;
  overflow: hidden;
}
.seg button {
  height: 36px;
  padding: 0 16px;
  border: none;
  background: transparent;
  color: var(--on-surface);
  font-size: 13px;
  font-weight: 500;
}
.seg button + button {
  border-left: 1px solid var(--outline);
}
.seg button.on {
  background: var(--secc);
  color: var(--on-secc);
}
@media (max-width: 760px) {
  .status {
    width: auto;
    text-align: left;
  }
}
</style>
