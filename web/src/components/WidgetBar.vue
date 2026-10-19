<script setup lang="ts">
// Live resources in a bar on the right of wide screens. Each widget can be hidden.
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import Icon from './Icon.vue'
import M3Checkbox from './M3Checkbox.vue'
import M3Dialog from './M3Dialog.vue'
import MetricCard from './MetricCard.vue'
import { api, type Container, type Security, type UpdateStatus } from '@/lib/api'
import { session } from '@/lib/session'
import { barOpen, setVisible, visible, WIDGETS } from '@/lib/widgets'
import { bytes, duration, pct, rate } from '@/lib/format'

const editing = ref(false)
const containers = ref<Container[] | null>(null)
const update = ref<UpdateStatus | null>(null)
const security = ref<Security | null>(null)

const snap = computed(() => session.snapshot)
const memPct = computed(() => (snap.value?.memTotal ? (100 * snap.value.memUsed) / snap.value.memTotal : 0))
const swapPct = computed(() => (snap.value?.swapTotal ? (100 * snap.value.swapUsed) / snap.value.swapTotal : 0))
const running = computed(() => containers.value?.filter((c) => c.state === 'running').length ?? 0)
const troubled = computed(
  () => containers.value?.filter((c) => c.health === 'unhealthy' || c.state === 'restarting' || c.state === 'dead').length ?? 0,
)
const packages = computed(() => security.value?.checks.find((c) => c.id === 'updates'))

async function load() {
  await Promise.allSettled([
    api.containers().then((r) => (containers.value = r.containers)),
    api.update().then((u) => (update.value = u)),
  ])
}
async function loadSecurity() {
  security.value = await api.security().catch(() => security.value)
}

let t1: ReturnType<typeof setInterval> | undefined
let t2: ReturnType<typeof setInterval> | undefined
onMounted(() => {
  load()
  loadSecurity()
  t1 = setInterval(load, 30_000)
  t2 = setInterval(loadSecurity, 300_000)
})
onBeforeUnmount(() => {
  clearInterval(t1)
  clearInterval(t2)
})
</script>

<template>
  <aside class="bar" aria-label="Widgets">
    <div class="head">
      <h2>Widgets</h2>
      <button class="icon-btn st" type="button" aria-label="Choose the widgets" v-tip="'Choose the widgets'" @click="editing = true">
        <Icon name="settings" :size="18" />
      </button>
      <button class="icon-btn st" type="button" aria-label="Hide the widgets" v-tip="'Hide the bar'" @click="barOpen = false">
        <Icon name="close" :size="18" />
      </button>
    </div>

    <MetricCard
      v-if="visible('cpu')"
      compact
      :label="`CPU · ${session.host?.cores ?? '?'} cores`"
      :value="snap ? pct(snap.cpu) : '…'"
      :percent="snap?.cpu ?? 0"
      :sub="snap ? `Load ${snap.load.map((l) => l.toFixed(2)).join(' · ')}` : ''"
      shape="cookie"
      tint="var(--pc)"
    />
    <MetricCard
      v-if="visible('memory')"
      compact
      label="Memory"
      :value="snap ? pct(memPct) : '…'"
      :percent="memPct"
      :sub="snap ? `${bytes(snap.memUsed)} of ${bytes(snap.memTotal)}` : ''"
      shape="clover"
      tint="var(--secc)"
    />
    <MetricCard
      v-if="visible('swap') && snap?.swapTotal"
      compact
      label="Swap"
      :value="pct(swapPct)"
      :percent="swapPct"
      :sub="`${bytes(snap.swapUsed)} of ${bytes(snap.swapTotal)}`"
      shape="flower"
      tint="var(--tc)"
    />
    <template v-if="visible('disks')">
      <MetricCard
        v-for="(d, i) in snap?.disks ?? []"
        :key="d.mount"
        compact
        :label="`Disk ${d.mount}`"
        :value="pct((100 * d.used) / d.total)"
        :percent="(100 * d.used) / d.total"
        :sub="`${bytes(d.total - d.used)} free of ${bytes(d.total)}`"
        :shape="i % 2 ? 'cookie' : 'sunny'"
        :tint="i % 2 ? 'var(--secc)' : 'var(--tc)'"
      />
    </template>
    <MetricCard
      v-if="visible('network') && snap"
      compact
      label="Network"
      :value="rate(snap.netRx)"
      :sub="`Down ${rate(snap.netRx)} · up ${rate(snap.netTx)}`"
      shape="clover"
      tint="var(--pc)"
    />
    <MetricCard
      v-if="visible('system') && snap"
      compact
      label="Load"
      :value="snap.load[0].toFixed(2)"
      :sub="`${snap.load.map((l) => l.toFixed(2)).join(' · ')} · up ${duration(snap.uptime)}`"
      shape="flower"
      tint="var(--secc)"
    />
    <RouterLink v-if="visible('containers')" to="/containers" class="plain-link">
      <MetricCard
        compact
        label="Containers"
        :value="containers ? `${running}/${containers.length}` : '…'"
        :percent="containers?.length ? (100 * running) / containers.length : 0"
        :sub="troubled ? `${troubled} in trouble` : containers ? 'All fine' : 'Docker not reachable yet'"
        shape="sunny"
        tint="var(--pc)"
      />
    </RouterLink>
    <RouterLink v-if="visible('updates')" to="/updates" class="plain-link">
      <MetricCard
        compact
        label="Updates"
        :value="update?.available ? (update.latest?.tag ?? 'New') : 'Up to date'"
        :sub="`Agent ${update?.current ?? session.version}${packages ? ` · ${packages.detail.split(':')[0]}` : ''}`"
        shape="cookie"
        tint="var(--tc)"
      />
    </RouterLink>

    <M3Dialog v-model="editing" title="Widgets">
      <p class="muted small" style="margin: 0">Shown in the bar of this browser. The Overview leaves out the cards the bar shows.</p>
      <div class="choices">
        <M3Checkbox v-for="w in WIDGETS" :key="w.id" :model-value="visible(w.id)" :label="w.label" @update:model-value="setVisible(w.id, $event)" />
      </div>
      <template #actions>
        <button class="btn btn-filled st" type="button" @click="editing = false">Done</button>
      </template>
    </M3Dialog>
  </aside>
</template>

<style scoped>
.bar {
  position: sticky;
  top: 0;
  flex: 0 0 300px;
  height: 100vh;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding: 20px 16px 24px;
  border-left: 1px solid var(--sch);
}
/* cards clip their decoration, so they must not shrink to fit the column */
.bar > * {
  flex-shrink: 0;
}
.head {
  display: flex;
  align-items: center;
  gap: 4px;
  margin-bottom: 6px;
}
.head h2 {
  flex: 1;
  font-size: 18px;
  font-weight: 500;
}
.plain-link {
  display: block;
  color: inherit;
  text-decoration: none;
  border-radius: 24px;
}
.plain-link:hover :deep(.metric) {
  background: var(--sch);
}
.choices {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(180px, 1fr));
  gap: 4px 12px;
}
</style>
