<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import Icon from '@/components/Icon.vue'
import StatusDot, { type Tone } from '@/components/StatusDot.vue'
import LogViewer from '@/components/LogViewer.vue'
import ScriptRunDialog from '@/components/ScriptRunDialog.vue'
import RebuildDialog from '@/components/RebuildDialog.vue'
import GroupBadge from '@/components/GroupBadge.vue'
import IconPicker from '@/components/IconPicker.vue'
import M3Dialog from '@/components/M3Dialog.vue'
import { appearance } from '@/lib/appearance'
import { groupTitle, setGroupTitle } from '@/lib/groupIcons'
import { openGroups, toggleGroup } from '@/lib/openGroups'
import { api, type Container, type ContainerStats, type Script } from '@/lib/api'
import { bytes, image } from '@/lib/format'
import { confirm, toast, toastError } from '@/lib/ui'

const list = ref<Container[]>([])
const stats = ref<Record<string, ContainerStats>>({})
const loading = ref(true)
const error = ref('')
const query = ref('')
const busy = ref<Record<string, boolean>>({})
const logsFor = ref<Container | null>(null)
const logsOpen = computed({ get: () => logsFor.value !== null, set: (v) => !v && (logsFor.value = null) })
const loadLogs = () => api.containerLogs(logsFor.value!.id).then((r) => r.logs)


// GitHub links and scripts of each compose folder
const scripts = ref<Script[]>([])
// Groups are collapsed by default; the open ones are remembered in this browser.
const isOpen = (name: string) => query.value.trim() !== '' || openGroups.value.has(name)
const toggle = toggleGroup

// Renaming a group: the title is shared, the compose name stays in parentheses.
const renameFor = ref<string | null>(null)
const renameOpen = computed({ get: () => renameFor.value !== null, set: (v) => !v && (renameFor.value = null) })
const newTitle = ref('')
function startRename(project: string) {
  renameFor.value = project
  newTitle.value = groupTitle(project)
}
async function saveTitle(title = newTitle.value.trim()) {
  const p = renameFor.value
  if (!p) return
  try {
    await setGroupTitle(p, title)
    renameFor.value = null
  } catch (e) {
    toastError(e)
  }
}
const iconFor = ref<string | null>(null)
const iconOpen = computed({ get: () => iconFor.value !== null, set: (v) => !v && (iconFor.value = null) })

const rebuildFor = ref<Container | null>(null)
const rebuildOpen = ref(false)
const runFor = ref<Script | null>(null)
const runOpen = computed({ get: () => runFor.value !== null, set: (v) => !v && (runFor.value = null) })

// Groups that are panel apps link to their page.
const appIds = ref(new Set<string>())
const scriptsFor = (dir?: string) => (dir ? scripts.value.filter((s) => s.dir === dir) : [])

async function loadLinks() {
  const [s, a] = await Promise.allSettled([api.scripts(), api.apps()])
  if (s.status === 'fulfilled') scripts.value = s.value.scripts
  if (a.status === 'fulfilled') appIds.value = new Set(a.value.apps.map((x) => x.id))
}

const groups = computed(() => {
  const q = query.value.trim().toLowerCase()
  const map = new Map<string, Container[]>()
  for (const c of list.value) {
    if (q && !c.name.toLowerCase().includes(q) && !c.image.toLowerCase().includes(q) && !(c.project ?? '').includes(q)) continue
    // Deployed apps group under their name; compose stacks under their project.
    const key = c.app || c.project || 'Standalone'
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
  loadLinks()
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

    <section v-for="([name, items], gi) in groups" :key="name" class="card group" :class="{ open: isOpen(name) }">
      <div class="ghead">
        <button v-if="name !== 'Standalone'" type="button" class="badge-btn st" :aria-label="`Change the icon of ${name}`" v-tip="'Change the icon'" @click="iconFor = name">
          <GroupBadge :project="name" :index="gi" />
        </button>
        <GroupBadge v-else :project="name" :index="gi" />
        <button v-if="name !== 'Standalone'" type="button" class="icon-btn st ren" :aria-label="`Rename ${groupTitle(name)}`" v-tip="'Rename'" @click="startRename(name)">
          <Icon name="edit" :size="16" />
        </button>
        <button type="button" class="gtoggle st" :aria-expanded="isOpen(name)" :aria-controls="`grp-${gi}`" @click="toggle(name)">
          <span class="gtitle">
            <span class="gname">{{ groupTitle(name) }}<span v-if="appearance.groupTitles?.[name]" class="orig"> ({{ name }})</span></span>
            <code v-if="items[0]?.workDir" class="path plain">{{ items[0].workDir }}</code>
          </span>
          <span class="dots" aria-hidden="true"><StatusDot v-for="c in items.slice(0, 12)" :key="c.id" :tone="tone(c)" :label="c.state" /></span>
          <span class="chip">{{ items.length }}</span>
          <Icon name="chevron" :size="20" class="chev" />
        </button>
        <RouterLink v-if="appIds.has(name)" :to="`/apps/${name}`" class="btn btn-text st app-link" v-tip="'Deployments, settings and logs of this app'">
          <Icon name="apps" :size="16" /> Open app
        </RouterLink>
      </div>
      <div :id="`grp-${gi}`" class="gbody" :inert="!isOpen(name)">
      <div class="ginner">
      <div v-if="scriptsFor(items[0]?.workDir).length" class="links">
        <button v-for="sc in scriptsFor(items[0]?.workDir)" :key="sc.name" class="btn btn-outlined st script-btn" type="button" v-tip="sc.target" @click="runFor = sc">
          <Icon name="terminal" :size="16" /> {{ sc.title || sc.name }}<span v-if="sc.running" class="dotr" />
        </button>
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
          <button v-if="c.service" class="icon-btn st" type="button" :aria-label="`Rebuild ${c.name}`" v-tip="'Rebuild: newer image or new build, then recreate'" @click="(rebuildFor = c), (rebuildOpen = true)">
            <Icon name="wrench" :size="18" />
          </button>
          <button class="icon-btn st" type="button" :aria-label="`Logs of ${c.name}`" @click="logsFor = c">
            <Icon name="logs" :size="20" />
          </button>
        </span>
      </div>
      </div>
      </div>
    </section>

    <M3Dialog v-model="renameOpen" title="Rename the group">
      <p class="muted small" style="margin: 0">Everyone sees this name, on the panel and in the app. Compose keeps <code>{{ renameFor }}</code>.</p>
      <form @submit.prevent="saveTitle()">
        <label class="field"><input v-model="newTitle" :placeholder="renameFor ?? ''" aria-label="Name shown" maxlength="60" /></label>
      </form>
      <template #actions>
        <button v-if="renameFor && appearance.groupTitles?.[renameFor]" class="btn btn-text st" type="button" @click="saveTitle('')">Reset</button>
        <button class="btn btn-text st" type="button" @click="renameOpen = false">Cancel</button>
        <button class="btn btn-filled st" type="button" @click="saveTitle()">Save</button>
      </template>
    </M3Dialog>
    <IconPicker v-if="iconFor" v-model="iconOpen" :project="iconFor" />
    <RebuildDialog v-if="rebuildFor" v-model="rebuildOpen" :container="rebuildFor" @done="load" />
    <ScriptRunDialog v-model="runOpen" :script="runFor" @done="loadLinks" />
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
.ghead {
  display: flex;
  align-items: center;
  gap: 12px;
}
.badge-btn {
  display: grid;
  padding: 0;
  border: none;
  border-radius: 12px;
  background: none;
}
.gtoggle {
  flex: 1;
  min-width: 0;
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 6px 8px;
  border: none;
  border-radius: 16px;
  background: none;
  color: inherit;
  text-align: left;
  font: inherit;
}
.gtitle {
  flex: 1;
  min-width: 0;
  display: flex;
  align-items: baseline;
  flex-wrap: wrap;
  gap: 2px 12px;
}
.gname {
  font-size: 20px;
  font-weight: 500;
}
.orig {
  font-size: 14px;
  font-weight: 400;
  color: var(--on-sv);
}
.ren {
  width: 32px;
  height: 32px;
}
.dots {
  display: flex;
  gap: 4px;
}
.chev {
  flex-shrink: 0;
  transition: rotate 0.25s var(--ease);
}
.group.open .chev {
  rotate: 180deg;
}
/* Animates to the content height without measuring it */
.gbody {
  display: grid;
  grid-template-rows: 0fr;
  transition: grid-template-rows 0.3s var(--ease-decel);
}
.group.open .gbody {
  grid-template-rows: 1fr;
}
.ginner {
  overflow: hidden;
  min-height: 0;
}
.ginner > :first-child {
  margin-top: 12px;
}
.links {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
  margin: -4px 0 8px;
  padding-bottom: 10px;
  border-bottom: 1px solid var(--sch);
}
.links .spacer {
  flex: 1;
}
.script-btn {
  height: 34px;
  padding: 0 14px;
  font-size: 13px;
}
.dotr {
  flex: none;
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--warn);
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
  gap: 6px;
}
@media (max-width: 760px) {
  .stat {
    width: auto;
    text-align: left;
  }
}
</style>
