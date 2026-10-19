<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import Icon from '@/components/Icon.vue'
import M3Shape from '@/components/M3Shape.vue'
import StatusDot from '@/components/StatusDot.vue'
import { api, streamScript, type Script, type ScriptRun } from '@/lib/api'
import { ago, dateTime } from '@/lib/format'
import { confirm, toast, toastError } from '@/lib/ui'
import type { ShapeKind } from '@/lib/shapes'

const scripts = ref<Script[]>([])
const runs = ref<ScriptRun[]>([])
const dir = ref('/etc/vps-admin/scripts')
const loading = ref(true)
const args = ref<Record<string, string>>({})
const busy = ref<Record<string, boolean>>({})

// Output panel: the live run or a run from the history
const shown = ref<{ title: string; text: string; live: boolean; exit?: number; error?: string } | null>(null)
const outEl = ref<HTMLElement>()
let closeStream: (() => void) | undefined

const SHAPES: ShapeKind[] = ['cookie', 'clover', 'sunny', 'flower']
const recent = computed(() => runs.value.slice(0, 20))

async function load() {
  try {
    const res = await api.scripts()
    scripts.value = res.scripts
    runs.value = res.runs
    dir.value = res.dir
  } catch (e) {
    toastError(e)
  } finally {
    loading.value = false
  }
}

async function scrollDown() {
  await nextTick()
  outEl.value?.scrollTo({ top: outEl.value.scrollHeight })
}

function follow(s: Script) {
  closeStream?.()
  shown.value = { title: s.name, text: '', live: true }
  closeStream = streamScript(s.name, {
    output: (chunk) => {
      if (shown.value) shown.value.text += chunk
      scrollDown()
    },
    end: (r) => {
      if (shown.value) Object.assign(shown.value, { live: false, exit: r.exitCode, error: r.error })
      load()
    },
  })
}

async function run(s: Script) {
  const a = (args.value[s.name] ?? '').trim()
  const ok = await confirm({
    title: `Run ${s.name}?`,
    body: `${s.target}${a ? ` ${a}` : ''}\nIt runs as the vps-admin user (Docker access, no root), for 30 minutes at most.`,
    action: 'Run',
  })
  if (!ok) return
  busy.value[s.name] = true
  try {
    await api.runScript(s.name, a)
    await load()
    follow(s)
  } catch (e) {
    toastError(e)
  } finally {
    busy.value[s.name] = false
  }
}

async function stop(s: Script) {
  try {
    await api.stopScript(s.name)
    toast(`${s.name} stopped`)
  } catch (e) {
    toastError(e)
  }
}

async function showRun(r: ScriptRun) {
  closeStream?.()
  try {
    const { output } = await api.scriptRunOutput(r.id)
    shown.value = { title: `${r.script} · ${dateTime(r.started)}`, text: output || 'No output.', live: false, exit: r.exitCode, error: r.error }
    scrollDown()
  } catch (e) {
    toastError(e)
  }
}

const runTone = (r: ScriptRun) => (r.finished == null ? 'warn' : r.exitCode === 0 && !r.error ? 'ok' : 'bad')

onMounted(load)
onBeforeUnmount(() => closeStream?.())
</script>

<template>
  <div class="page">
    <div class="page-title">
      <h1>Scripts</h1>
      <button class="btn btn-tonal st" type="button" @click="load"><Icon name="restart" :size="18" /> Refresh</button>
    </div>

    <p v-if="loading" class="muted">Loading…</p>

    <section v-else-if="!scripts.length" class="card empty">
      <M3Shape kind="sunny" :size="64" fill="var(--tc)" />
      <h2>No script yet</h2>
      <p class="muted">Link any executable file from the server, it shows up here with a Run button:</p>
      <code class="cmd">sudo vps-admin link /path/to/update-app.sh</code>
      <p class="muted small">Add <code>-name</code> to choose its name. A comment on the second line becomes its description.</p>
    </section>

    <div v-else class="cols">
      <div class="list">
        <section v-for="(s, i) in scripts" :key="s.name" class="card script">
          <div class="head">
            <M3Shape :kind="SHAPES[i % SHAPES.length]!" :size="40" :fill="i % 2 ? 'var(--secc)' : 'var(--pc)'" />
            <div class="grow">
              <div class="name">
                {{ s.name }}
                <span v-if="s.running" class="chip run-chip">running</span>
                <span v-if="s.missing" class="chip bad-chip">file missing</span>
                <span v-else-if="!s.executable" class="chip bad-chip">not executable</span>
              </div>
              <div v-if="s.description" class="muted small">{{ s.description }}</div>
              <code class="target">{{ s.target }}</code>
            </div>
          </div>
          <div class="actions">
            <label class="field args">
              <input v-model="args[s.name]" placeholder="Arguments (optional)" :aria-label="`Arguments for ${s.name}`" spellcheck="false" @keydown.enter="run(s)" />
            </label>
            <button v-if="s.running" class="btn btn-outlined st" type="button" @click="follow(s)">Output</button>
            <button v-if="s.running" class="btn btn-danger st" type="button" @click="stop(s)"><Icon name="stop" :size="16" /> Stop</button>
            <button v-else class="btn btn-filled st" type="button" :disabled="busy[s.name] || s.missing || !s.executable" @click="run(s)">
              <Icon name="play" :size="18" /> Run
            </button>
          </div>
        </section>
        <p class="muted small">Linked in <code>{{ dir }}</code> with <code>sudo vps-admin link FILE</code>. Remove one with <code>sudo vps-admin unlink NAME</code>.</p>
      </div>

      <div class="side">
        <section class="card out">
          <div class="card-head">
            <h2>{{ shown?.title ?? 'Output' }}</h2>
            <span v-if="shown?.live" class="chip run-chip">live</span>
            <span v-else-if="shown && shown.exit !== undefined" class="chip" :class="shown.exit === 0 && !shown.error ? 'ok-chip' : 'bad-chip'">exit {{ shown.exit }}</span>
          </div>
          <div v-if="shown?.error" class="err small"><Icon name="alert" :size="16" /> {{ shown.error }}</div>
          <pre ref="outEl" class="output">{{ shown ? shown.text || 'Waiting for output…' : 'Run a script or pick one from the history.' }}</pre>
        </section>

        <section class="card">
          <div class="card-head"><h2>History</h2></div>
          <p v-if="!recent.length" class="muted small">Nothing has run yet.</p>
          <button v-for="r in recent" :key="r.id" type="button" class="hist st" @click="showRun(r)">
            <StatusDot :tone="runTone(r)" :label="r.finished == null ? 'running' : `exit ${r.exitCode ?? '?'}`" />
            <span class="grow ellipsis"><strong>{{ r.script }}</strong>{{ r.args ? `\u00a0${r.args}` : '' }}</span>
            <span class="muted small">{{ r.device || 'assistant' }} · {{ ago(r.started) }}</span>
          </button>
        </section>
      </div>
    </div>
  </div>
</template>

<style scoped>
.empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 10px;
  padding: 36px;
  text-align: center;
}
.empty p {
  margin: 0;
}
.cmd {
  padding: 12px 16px;
  border-radius: 12px;
  background: var(--scc);
  color: var(--primary);
  font-size: 14px;
}
.cols {
  display: flex;
  flex-wrap: wrap;
  gap: 16px;
  align-items: flex-start;
}
.list {
  flex: 1 1 460px;
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.side {
  flex: 1.2 1 480px;
  display: flex;
  flex-direction: column;
  gap: 12px;
  min-width: 0;
}
.script {
  display: flex;
  flex-direction: column;
  gap: 14px;
}
.head {
  display: flex;
  gap: 14px;
  align-items: flex-start;
}
.name {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 17px;
  font-weight: 500;
}
.target {
  display: block;
  margin-top: 4px;
  font-size: 12px;
  color: var(--on-sv);
  word-break: break-all;
}
.actions {
  display: flex;
  gap: 8px;
  align-items: center;
}
.args {
  flex: 1;
  height: 44px;
}
.args input {
  font-family: var(--mono);
  font-size: 13px;
}
.run-chip {
  background: var(--warn);
  color: var(--on-warn);
}
.bad-chip {
  background: var(--bad);
  color: var(--on-bad);
}
.ok-chip {
  background: var(--ok);
  color: var(--on-ok);
}
.output {
  margin: 0;
  height: 380px;
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
.err {
  display: flex;
  gap: 8px;
  margin-bottom: 10px;
  padding: 10px 12px;
  border-radius: 12px;
  background: var(--ec);
  color: var(--on-ec);
}
.hist {
  display: flex;
  align-items: center;
  gap: 12px;
  width: 100%;
  padding: 10px 8px;
  border: none;
  border-radius: 12px;
  background: transparent;
  color: var(--on-surface);
  text-align: left;
  font-size: 14px;
}
</style>
