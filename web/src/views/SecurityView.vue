<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import M3Shape from '@/components/M3Shape.vue'
import Icon from '@/components/Icon.vue'
import ShapeLoader from '@/components/ShapeLoader.vue'
import { api, type Check, type Security } from '@/lib/api'
import { ago } from '@/lib/format'
import { toastError } from '@/lib/ui'

const report = ref<Security | null>(null)
const loading = ref(false)

const GROUPS = [
  { id: 'ssh', title: 'SSH', shape: 'cookie' as const, tint: 'var(--pc)' },
  { id: 'network', title: 'Network', shape: 'clover' as const, tint: 'var(--secc)' },
  { id: 'updates', title: 'Updates', shape: 'sunny' as const, tint: 'var(--tc)' },
]
const byGroup = (g: string): Check[] => report.value?.checks.filter((c) => c.group === g) ?? []
const counts = computed(() => {
  const c = { ok: 0, warn: 0, fail: 0, unknown: 0 }
  for (const x of report.value?.checks ?? []) c[x.status]++
  return c
})
const publicPorts = computed(() => report.value?.ports.filter((p) => p.public) ?? [])
const localPorts = computed(() => report.value?.ports.filter((p) => !p.public) ?? [])
const LABEL = { ok: 'Good', warn: 'To improve', fail: 'To fix', unknown: 'Unknown' }

async function load(refresh = false) {
  loading.value = true
  try {
    report.value = await api.security(refresh)
  } catch (e) {
    toastError(e)
  } finally {
    loading.value = false
  }
}
onMounted(() => load())
</script>

<template>
  <div class="page">
    <div class="page-title">
      <h1>Security</h1>
      <span v-if="report" class="muted small">Checked {{ ago(report.checked) }}</span>
      <button class="btn btn-tonal st" type="button" :disabled="loading" :aria-busy="loading" @click="load(true)">
        <ShapeLoader v-if="loading" small label="Running the checks" />
        <Icon v-else name="restart" small />
        Check again
      </button>
    </div>

    <section class="card summary">
      <div class="score">
        <M3Shape kind="sunny" :size="120" fill="var(--tc)" />
        <span>{{ report ? report.score : '…' }}<small>/100</small></span>
      </div>
      <div class="grow">
        <h2>{{ !report ? 'Running the checks…' : counts.fail ? 'Some things need fixing' : counts.warn ? 'Good, a few improvements left' : 'Well protected' }}</h2>
        <div class="tally">
          <span class="chip t-fail">{{ counts.fail }} to fix</span>
          <span class="chip t-warn">{{ counts.warn }} to improve</span>
          <span class="chip t-ok">{{ counts.ok }} good</span>
          <span v-if="counts.unknown" class="chip chip-outline">{{ counts.unknown }} unknown</span>
        </div>
        <p class="muted small">
          The checks only read the configuration. Fixes are listed with each item; the assistant will be
          able to apply them with your approval.
        </p>
      </div>
    </section>

    <section v-for="g in GROUPS" :key="g.id" class="card">
      <div class="card-head">
        <M3Shape :kind="g.shape" :size="28" :fill="g.tint" />
        <h2>{{ g.title }}</h2>
      </div>
      <div v-for="c in byGroup(g.id)" :key="c.id" class="check">
        <span class="mark" :class="c.status" :aria-label="LABEL[c.status]">{{ c.status === 'ok' ? '✓' : c.status === 'unknown' ? '?' : '!' }}</span>
        <div class="grow">
          <div class="title">{{ c.title }}</div>
          <div class="muted small">{{ c.detail }}</div>
          <div v-if="c.fix && c.status !== 'ok'" class="fix small"><Icon name="info" :size="16" /> {{ c.fix }}</div>
        </div>
        <span class="state small" :class="c.status">{{ LABEL[c.status] }}</span>
      </div>
    </section>

    <section class="card">
      <div class="card-head">
        <M3Shape kind="flower" :size="28" fill="var(--pc)" />
        <h2>Listening ports</h2>
        <span class="muted small">What the firewall then filters</span>
      </div>
      <div class="ports">
        <div>
          <h3 class="small muted">All interfaces</h3>
          <p v-if="!publicPorts.length" class="muted small">None</p>
          <div v-for="p in publicPorts" :key="`${p.proto}${p.port}`" class="port">
            <code>{{ p.port }}/{{ p.proto }}</code><span class="muted small ellipsis">{{ p.process || 'unknown process' }}</span>
          </div>
        </div>
        <div>
          <h3 class="small muted">Local or private only</h3>
          <p v-if="!localPorts.length" class="muted small">None</p>
          <div v-for="p in localPorts" :key="`${p.proto}${p.port}${p.addr}`" class="port">
            <code>{{ p.port }}/{{ p.proto }}</code><span class="muted small ellipsis">{{ p.process || p.addr }}</span>
          </div>
        </div>
      </div>
    </section>
  </div>
</template>

<style scoped>
.summary {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 24px;
}
.summary h2 {
  font-size: 24px;
  margin-bottom: 10px;
}
.summary p {
  margin: 12px 0 0;
}
.score {
  position: relative;
  width: 120px;
  height: 120px;
  display: grid;
  place-items: center;
}
.score svg {
  position: absolute;
  inset: 0;
  animation: spin 40s linear infinite;
}
@keyframes spin {
  to {
    rotate: 360deg;
  }
}
.score span {
  position: relative;
  font-size: 34px;
  font-weight: 600;
  color: var(--on-tc);
}
.score small {
  font-size: 14px;
  font-weight: 500;
}
.tally {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
.t-fail {
  background: var(--bad);
  color: var(--on-bad);
}
.t-warn {
  background: var(--warn);
  color: var(--on-warn);
}
.t-ok {
  background: var(--ok);
  color: var(--on-ok);
}
.check {
  display: flex;
  align-items: flex-start;
  gap: 14px;
  padding: 14px 4px;
  border-top: 1px solid var(--sch);
}
.check:first-of-type {
  border-top: none;
}
.title {
  font-size: 16px;
  font-weight: 500;
}
.fix {
  display: flex;
  gap: 8px;
  align-items: flex-start;
  margin-top: 8px;
  padding: 10px 12px;
  border-radius: 12px;
  background: var(--sch);
}
.fix svg {
  flex-shrink: 0;
  margin-top: 1px;
  color: var(--primary);
}
.mark {
  width: 26px;
  height: 26px;
  border-radius: 13px;
  display: grid;
  place-items: center;
  flex-shrink: 0;
  font-size: 14px;
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
.state {
  width: 90px;
  text-align: right;
  color: var(--on-sv);
}
.ports {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(260px, 1fr));
  gap: 20px;
}
.ports h3 {
  margin-bottom: 8px;
  font-weight: 500;
}
.port {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 8px 0;
  border-top: 1px solid var(--sch);
}
.port code {
  width: 90px;
  color: var(--primary);
  font-size: 13px;
}
@media (max-width: 760px) {
  .state {
    display: none;
  }
}
</style>
