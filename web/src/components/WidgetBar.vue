<script setup lang="ts">
// Live resources in a bar on the right of wide screens. Each widget can be hidden.
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import Icon from './Icon.vue'
import M3Checkbox from './M3Checkbox.vue'
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
const level = (p: number) => (p >= 90 ? 'bad' : p >= 75 ? 'warn' : '')

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
      <button class="icon-btn st" type="button" :class="{ on: editing }" :aria-pressed="editing" aria-label="Choose the widgets" v-tip="'Choose the widgets'" @click="editing = !editing">
        <Icon name="settings" :size="18" />
      </button>
      <button class="icon-btn st" type="button" aria-label="Hide the widgets" v-tip="'Hide the bar'" @click="barOpen = false">
        <Icon name="close" :size="18" />
      </button>
    </div>

    <div v-if="editing" class="w edit">
      <M3Checkbox v-for="w in WIDGETS" :key="w.id" :model-value="visible(w.id)" :label="w.label" @update:model-value="setVisible(w.id, $event)" />
    </div>

    <div v-if="visible('cpu')" class="w">
      <div class="top"><span>CPU · {{ session.host?.cores ?? '?' }} cores</span><strong>{{ snap ? pct(snap.cpu) : '…' }}</strong></div>
      <div class="track"><div class="fill" :class="level(snap?.cpu ?? 0)" :style="{ width: `${Math.min(snap?.cpu ?? 0, 100)}%` }" /></div>
    </div>

    <div v-if="visible('memory')" class="w">
      <div class="top"><span>Memory</span><strong>{{ snap ? pct(memPct) : '…' }}</strong></div>
      <div class="track"><div class="fill" :class="level(memPct)" :style="{ width: `${memPct}%` }" /></div>
      <div v-if="snap" class="sub">{{ bytes(snap.memUsed) }} of {{ bytes(snap.memTotal) }}</div>
    </div>

    <div v-if="visible('swap') && snap?.swapTotal" class="w">
      <div class="top"><span>Swap</span><strong>{{ pct(swapPct) }}</strong></div>
      <div class="track"><div class="fill" :class="level(swapPct)" :style="{ width: `${swapPct}%` }" /></div>
      <div class="sub">{{ bytes(snap.swapUsed) }} of {{ bytes(snap.swapTotal) }}</div>
    </div>

    <div v-if="visible('disks') && snap?.disks.length" class="w">
      <div class="top"><span>Disks</span></div>
      <div v-for="d in snap.disks" :key="d.mount" class="disk">
        <div class="top small"><code class="plain">{{ d.mount }}</code><span>{{ pct((100 * d.used) / d.total) }}</span></div>
        <div class="track"><div class="fill" :class="level((100 * d.used) / d.total)" :style="{ width: `${(100 * d.used) / d.total}%` }" /></div>
        <div class="sub">{{ bytes(d.total - d.used) }} free of {{ bytes(d.total) }}</div>
      </div>
    </div>

    <div v-if="visible('network') && snap" class="w">
      <div class="top"><span>Network</span></div>
      <div class="net">
        <span><Icon name="updates" :size="16" /> {{ rate(snap.netRx) }}</span>
        <span><Icon name="updates" :size="16" class="up" /> {{ rate(snap.netTx) }}</span>
      </div>
    </div>

    <div v-if="visible('system') && snap" class="w">
      <div class="top"><span>Load</span><strong>{{ snap.load[0].toFixed(2) }}</strong></div>
      <div class="sub">{{ snap.load.map((l) => l.toFixed(2)).join(' · ') }} · up {{ duration(snap.uptime) }}</div>
    </div>

    <RouterLink v-if="visible('containers')" to="/containers" class="w link">
      <div class="top"><span>Containers</span><strong>{{ containers ? `${running}/${containers.length}` : '…' }}</strong></div>
      <div class="sub" :class="{ alert: troubled }">{{ troubled ? `${troubled} in trouble` : containers ? 'All fine' : 'Docker not reachable yet' }}</div>
    </RouterLink>

    <RouterLink v-if="visible('updates')" to="/updates" class="w link">
      <div class="top"><span>Updates</span><strong v-if="update?.available" class="new">{{ update.latest?.tag }}</strong></div>
      <div class="sub">Agent {{ update?.current ?? session.version }}{{ update?.available ? ' · update ready' : '' }}</div>
      <div v-if="packages" class="sub clamp">{{ packages.detail }}</div>
    </RouterLink>
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
.icon-btn.on {
  background: var(--secc);
  color: var(--on-secc);
}
.w {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 14px 16px;
  border-radius: 20px;
  background: var(--sc);
  color: var(--on-surface);
  text-decoration: none;
}
.w.link:hover {
  background: var(--sch);
}
.edit {
  gap: 2px;
}
.top {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 8px;
  font-size: 14px;
  color: var(--on-sv);
}
.top strong {
  font-size: 20px;
  font-weight: 500;
  color: var(--on-surface);
}
.track {
  height: 6px;
  border-radius: 3px;
  background: var(--sch);
  overflow: hidden;
}
.fill {
  height: 100%;
  border-radius: 3px;
  background: var(--primary);
  transition: width 0.6s var(--ease-decel);
}
.fill.warn {
  background: var(--warn);
}
.fill.bad {
  background: var(--error);
}
.sub {
  font-size: 12px;
  color: var(--on-sv);
}
.clamp {
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}
.sub.alert {
  color: var(--error);
}
.disk {
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.disk + .disk {
  margin-top: 6px;
}
.net {
  display: flex;
  justify-content: space-between;
  font-size: 15px;
}
.net span {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}
.up {
  rotate: 180deg;
}
.new {
  color: var(--primary) !important;
  font-size: 15px !important;
}
</style>
