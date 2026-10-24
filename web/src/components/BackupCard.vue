<script setup lang="ts">
// Google Drive backups: connect the Drive once (rclone token), mount it, and let the
// panel write an encrypted archive of its state on a schedule.
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import Icon from '@/components/Icon.vue'
import M3Dialog from '@/components/M3Dialog.vue'
import M3Select from '@/components/M3Select.vue'
import M3Switch from '@/components/M3Switch.vue'
import ShapeLoader from '@/components/ShapeLoader.vue'
import StatusDot from '@/components/StatusDot.vue'
import { api, type BackupStatus } from '@/lib/api'
import { bytes, dateTime } from '@/lib/format'
import { confirm, toast, toastError } from '@/lib/ui'

const st = ref<BackupStatus | null>(null)
const loading = ref(true)
const busy = ref('')
const token = ref('')
const passphrase = ref('')
const editing = ref(false)

const path = ref('')
const hour = ref(3)
const keep = ref(7)
const kind = ref<'settings' | 'full'>('settings')
const enabled = ref(false)
const mountOn = ref(true)

const kindOptions = [
  { value: 'settings' as const, label: 'Panel state only', hint: 'Database, its key, secrets and settings' },
  { value: 'full' as const, label: 'Everything', hint: 'Also the app sources, the wallpaper and the Docker volumes' },
]

const restoreOpen = ref(false)
const restoreFile = ref('')
const restorePass = ref('')

let timer: ReturnType<typeof setTimeout> | undefined

const connected = computed(() => !!st.value?.hasToken && !!st.value?.hasPassphrase)
const tone = computed(() => {
  if (!st.value) return 'idle' as const
  if (st.value.running) return 'warn' as const
  return st.value.last?.error ? 'bad' : st.value.last?.ok ? 'ok' : 'idle'
})
// The newest archive first, even when a status cached before the agent sorted the list
// arrives: a restore almost always wants the last backup.
const archives = computed(() =>
  [...(st.value?.archives ?? [])].sort((a, b) => {
    const ta = Date.parse(a.time) || 0
    const tb = Date.parse(b.time) || 0
    return tb - ta || b.name.localeCompare(a.name)
  }),
)

async function load(withArchives = false) {
  const s = withArchives ? await api.backupArchives() : await api.backup()
  st.value = s
  if (!editing.value) {
    path.value = s.path
    hour.value = s.hour
    keep.value = s.keep
    kind.value = s.kind === 'full' ? 'full' : 'settings'
    enabled.value = s.enabled
    mountOn.value = s.mount
  }
  if (s.running) {
    timer = setTimeout(() => load(true).catch(() => {}), 2000)
  }
}

onMounted(async () => {
  try {
    await load()
  } catch (e) {
    toastError(e)
  } finally {
    loading.value = false
  }
})
onBeforeUnmount(() => timer && clearTimeout(timer))

function stopPolling() {
  if (timer) clearTimeout(timer)
  timer = undefined
}

async function save(patch: Record<string, unknown>, done: string) {
  busy.value = 'save'
  try {
    st.value = await api.backupSave(patch)
    editing.value = false
    token.value = ''
    passphrase.value = ''
    toast(done)
  } catch (e) {
    toastError(e)
  } finally {
    busy.value = ''
  }
}

function connect() {
  if (!token.value.trim() && !st.value?.hasToken) return toast('Paste the rclone token first', true)
  if (!passphrase.value && !st.value?.hasPassphrase) return toast('Choose a passphrase: it encrypts every archive', true)
  const patch: Record<string, unknown> = {}
  if (token.value.trim()) patch.token = token.value
  if (passphrase.value) patch.passphrase = passphrase.value
  return save(patch, 'Google Drive connected')
}

async function testDrive() {
  busy.value = 'test'
  try {
    const r = await api.backupTest()
    toast(r.detail)
  } catch (e) {
    toastError(e)
  } finally {
    busy.value = ''
  }
}

async function setMount(on: boolean) {
  busy.value = 'mount'
  try {
    st.value = await api.backupMount(on)
    toast(on ? 'Google Drive mounted' : 'Google Drive unmounted')
  } catch (e) {
    toastError(e)
  } finally {
    busy.value = ''
  }
}

// Mount at boot is a setting, not a mount command: save it as soon as the switch moves,
// like the app does.
function setMountBoot(on: boolean) {
  mountOn.value = on
  save({ mount: on }, on ? 'Drive mounts at boot' : 'No mount at boot')
}

async function run(kind: 'settings' | 'full') {
  busy.value = 'run'
  stopPolling()
  try {
    await api.backupRun(kind)
    toast('Backup started')
    await load(true)
  } catch (e) {
    toastError(e)
  } finally {
    busy.value = ''
  }
}

async function refreshArchives() {
  try {
    await load(true)
  } catch (e) {
    toastError(e)
  }
}

function askRestore(file: string) {
  restoreFile.value = file
  restorePass.value = ''
  restoreOpen.value = true
}

async function remove(file: string) {
  const ok = await confirm({
    title: `Delete ${file}?`,
    body: 'The archive is removed from Google Drive, and a backup cannot be restored once it is gone.',
    action: 'Delete',
    danger: true,
  })
  if (!ok) return
  busy.value = 'delete:' + file
  try {
    st.value = await api.backupDelete(file)
    toast(`${file} deleted`)
  } catch (e) {
    toastError(e)
  } finally {
    busy.value = ''
  }
}

async function doRestore() {
  busy.value = 'restore'
  try {
    await api.backupRestore(restoreFile.value, restorePass.value)
    restoreOpen.value = false
    toast('Restoring: the panel restarts with the backup in a few seconds')
    stopPolling()
    setTimeout(() => location.reload(), 7000)
  } catch (e) {
    toastError(e)
  } finally {
    busy.value = ''
  }
}

function saveSchedule() {
  save(
    {
      path: path.value,
      hour: Math.min(23, Math.max(0, Math.round(hour.value || 0))),
      keep: Math.min(60, Math.max(1, Math.round(keep.value || 7))),
      kind: kind.value,
      enabled: enabled.value,
      mount: mountOn.value,
    },
    'Backup settings saved',
  )
}
</script>

<template>
  <div class="backup-stack">
    <section v-if="loading" class="card">
      <div class="loading-block"><ShapeLoader label="Loading backups" /></div>
    </section>

    <template v-else-if="st">
      <section v-if="st.warning" class="card warn-card">
        <p class="warn small">{{ st.warning }}</p>
      </section>

      <section v-if="st.restoreStaged" class="card">
        <div class="restore-note">
          <Icon name="history" :size="20" />
          <span>A restore is staged: the panel installs it at its next restart.</span>
        </div>
      </section>

      <section v-if="!connected || editing" class="card">
        <div class="card-head">
          <h2>Google Drive</h2>
        </div>
        <p class="muted small intro">
          On your computer, install rclone and run <code>rclone authorize "drive"</code>, then paste the JSON it prints below.
          The token is kept on this server and only sent to Google. The passphrase encrypts every archive: without it, a
          backup cannot be opened, and with it an archive gives back every secret the panel holds, so keep it in your password manager.
        </p>
        <label class="stack">
          <span class="small">rclone token</span>
          <textarea v-model="token" class="area" spellcheck="false" :placeholder="st.hasToken ? 'Saved: paste a new token to replace it' : '{&quot;access_token&quot;:&quot;…&quot;,&quot;refresh_token&quot;:&quot;…&quot;}'" />
        </label>
        <label class="stack">
          <span class="small">Passphrase (12 characters or more)</span>
          <input v-model="passphrase" class="area one" type="password" autocomplete="new-password" :placeholder="st.hasPassphrase ? 'Saved: paste a new passphrase to replace it' : ''" />
        </label>
        <div class="acts">
          <button class="btn btn-filled st" type="button" :disabled="busy === 'save'" @click="connect">
            {{ connected ? 'Save changes' : 'Connect the Drive' }}
          </button>
          <button v-if="connected" class="btn btn-text st" type="button" @click="editing = false; token = ''; passphrase = ''">Cancel</button>
        </div>
        <p v-if="st.hasPassphrase" class="muted small">Changing the passphrase makes the archives already on the Drive unreadable. Download them first if you need them. After changing the token, unmount and mount the Drive again so rclone uses it.</p>
      </section>

      <template v-else>
        <section class="card">
          <div class="card-head">
            <h2>Google Drive</h2>
            <StatusDot :tone="tone" :label="st.running ? 'Backup running' : st.last?.ok ? 'Last backup succeeded' : 'No backup yet'" />
          </div>

          <div class="settings">
            <div class="row">
              <div class="grow">
                <div>{{ st.mounted ? 'Mounted' : 'Connected, not mounted' }}</div>
                <div class="muted small">{{ st.mounted ? `Mounted at ${st.mountPoint}` : 'Archives go through rclone until it is mounted' }}</div>
              </div>
              <button class="btn btn-text st" type="button" :disabled="busy === 'test'" @click="testDrive">Test</button>
              <button class="btn btn-tonal st" type="button" :disabled="busy === 'mount'" @click="setMount(!st.mounted)">
                {{ st.mounted ? 'Unmount' : 'Mount' }}
              </button>
            </div>

            <div class="row">
              <div class="grow">
                <div>Mount at boot</div>
                <div class="muted small">The panel mounts the Drive again if the unit stops</div>
              </div>
              <M3Switch :model-value="mountOn" label="Mount at boot" :disabled="busy === 'save'" @update:model-value="setMountBoot" />
            </div>
          </div>

          <div class="acts">
            <button class="btn btn-text st" type="button" @click="editing = true">Change Drive token</button>
          </div>
        </section>

        <section class="card">
          <div class="card-head">
            <h2>Daily backup</h2>
          </div>

          <div class="settings">
            <div class="row">
              <div class="grow">
                <div>Enabled</div>
                <div class="muted small">The panel database, its key, the apps and their variables, in one encrypted archive</div>
              </div>
              <M3Switch v-model="enabled" label="Back up every day" />
            </div>

            <div class="row">
              <div class="grow">
                <div>What the daily backup keeps</div>
                <div class="muted small">
                  {{ kind === 'full'
                    ? 'The panel state plus app sources, wallpaper and Docker volumes. Containers are paused while volumes are copied.'
                    : 'The panel state: database, key, apps and their variables. Restoring it leaves the containers untouched.' }}
                </div>
              </div>
              <M3Select v-model="kind" class="kind" label="Daily archive" :options="kindOptions" />
            </div>
          </div>

          <div class="schedule">
            <label class="fld">
              <span>Folder on the Drive</span>
              <input v-model="path" placeholder="vps-admin" spellcheck="false" autocomplete="off" />
            </label>
            <label class="mini">At <input v-model.number="hour" type="number" min="0" max="23" /> :00</label>
            <label class="mini">Keep <input v-model.number="keep" type="number" min="1" max="60" /> archives</label>
          </div>

          <div class="acts">
            <button class="btn btn-outlined st" type="button" :disabled="busy === 'save'" @click="saveSchedule">Save settings</button>
          </div>
        </section>

        <section class="card">
          <div class="card-head">
            <h2>Back up now</h2>
          </div>
          <p class="muted small">Both write an encrypted archive and upload it to the Drive. The full one pauses the containers while it copies their volumes.</p>
          <div class="acts">
            <button class="btn btn-filled st" type="button" :disabled="busy === 'run' || st.running" @click="run('settings')">
              <Icon name="cloud" :size="18" /> {{ st.running ? st.progress || 'Backing up…' : 'Panel state' }}
            </button>
            <button class="btn btn-outlined st" type="button" :disabled="busy === 'run' || st.running" @click="run('full')">
              <Icon name="database" :size="18" /> Full backup
            </button>
          </div>
        </section>

        <section v-if="st.last" class="card">
          <div class="card-head">
            <h2>Last backup</h2>
            <StatusDot :tone="st.last.ok ? 'ok' : 'bad'" :label="st.last.ok ? 'Succeeded' : 'Failed'" />
            <span class="chip">{{ st.last.kind === 'full' ? 'Everything' : 'Panel state' }}</span>
          </div>
          <div class="last">
            <div class="grow">
              <div>{{ st.last.ok ? 'Last backup succeeded' : 'Last backup failed' }}</div>
              <div class="muted small">
                {{ dateTime(st.last.at) }} · {{ st.last.file || 'no file' }}<template v-if="st.last.size"> · {{ bytes(st.last.size) }}</template>
              </div>
              <div v-if="st.last.error" class="small err">{{ st.last.error }}</div>
            </div>
          </div>
        </section>

        <section class="card">
          <div class="card-head">
            <h2>Archives on the Drive</h2>
            <button class="icon-btn st" type="button" aria-label="Refresh archives" v-tip="'Refresh archives'" @click="refreshArchives">
              <Icon name="restart" :size="20" />
            </button>
          </div>
          <p v-if="!archives.length" class="muted small empty">None yet.</p>
          <div v-for="a in archives" :key="a.name" class="archive">
            <div class="grow">
              <div class="ellipsis"><code>{{ a.name }}</code></div>
              <div class="muted small">{{ dateTime(a.time) }}<template v-if="a.size"> · {{ bytes(a.size) }}</template></div>
            </div>
            <div class="archive-actions">
              <button class="btn btn-text st" type="button" :disabled="busy === 'restore'" @click="askRestore(a.name)">Restore</button>
              <button
                class="btn btn-text st del"
                type="button"
                :disabled="busy === 'delete:' + a.name || st.running"
                @click="remove(a.name)"
              >
                Delete
              </button>
            </div>
          </div>
        </section>
      </template>
    </template>

    <section v-else class="card">
      <p class="muted small">The backup status is unavailable. Reload the page, then check the agent's logs.</p>
    </section>

    <M3Dialog v-model="restoreOpen" title="Restore this backup">
      <p class="muted" style="margin: 0">
        The panel replaces its database and its key with <code>{{ restoreFile }}</code>, then restarts. A full archive also brings back the app
        folders, the wallpaper and the Docker volumes, with the containers paused while they are unpacked. The apps keep running until then; the
        panel comes back with the apps, devices and settings of the archive. The files it replaces are kept next to the database. Passkeys only
        keep working on the domain they were registered on.
      </p>
      <label class="stack" style="margin-top: 14px">
        <span class="small">Passphrase of the archive</span>
        <input v-model="restorePass" class="area one" type="password" autocomplete="current-password" />
      </label>
      <template #actions>
        <button class="btn btn-text st" type="button" @click="restoreOpen = false">Cancel</button>
        <button class="btn btn-filled st" type="button" :disabled="busy === 'restore' || !restorePass" @click="doRestore">Restore and restart</button>
      </template>
    </M3Dialog>
  </div>
</template>

<style scoped>
.backup-stack {
  display: flex;
  flex-direction: column;
  gap: 16px;
}
.warn-card .warn {
  margin: 0;
}
.warn {
  color: var(--warn);
}
.restore-note {
  display: flex;
  align-items: center;
  gap: 12px;
}
.intro {
  margin: 0 0 10px;
  max-width: 72ch;
}
.stack {
  display: flex;
  flex-direction: column;
  gap: 6px;
  margin-bottom: 10px;
}
.area {
  width: 100%;
  min-height: 96px;
  padding: 12px 14px;
  border: 1px solid var(--outline);
  border-radius: 12px;
  background: transparent;
  color: var(--on-surface);
  font: inherit;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 13px;
  resize: vertical;
}
.area:focus {
  outline: none;
  border-color: var(--primary);
  box-shadow: inset 0 0 0 1px var(--primary);
}
.area.one {
  min-height: 0;
  height: 46px;
  font-family: inherit;
  font-size: 14px;
  resize: none;
}
.settings .row:first-child {
  border-top: none;
}
.acts {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
  margin-top: 14px;
}
.last {
  display: flex;
  align-items: flex-start;
  gap: 14px;
}
.empty {
  margin: 6px 0 0;
}
.schedule {
  display: flex;
  flex-wrap: wrap;
  align-items: flex-end;
  gap: 12px 14px;
  margin-top: 14px;
}
.fld {
  flex: 1 1 240px;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.fld span {
  padding-left: 2px;
  font-size: 12px;
  color: var(--on-sv);
}
.fld input {
  height: 48px;
  padding: 0 14px;
  border: 1px solid var(--outline);
  border-radius: 12px;
  background: transparent;
  color: var(--on-surface);
  font: inherit;
}
.fld input:focus {
  outline: none;
  border-color: var(--primary);
  box-shadow: inset 0 0 0 1px var(--primary);
}
.mini {
  display: flex;
  align-items: center;
  gap: 6px;
  height: 48px;
  font-size: 13px;
  color: var(--on-sv);
}
.mini input {
  width: 64px;
  height: 40px;
  padding: 0 8px;
  border: 1px solid var(--outline);
  border-radius: 10px;
  background: transparent;
  color: var(--on-surface);
  font: inherit;
}
.kind {
  width: 260px;
}
.archive {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 10px 6px;
  flex-wrap: wrap;
}
.archive + .archive {
  border-top: 1px solid var(--sch);
}
.archive-actions {
  display: flex;
  align-items: center;
  gap: 2px;
  margin-left: auto;
}
.del {
  color: var(--bad);
}
.err {
  color: var(--bad);
  overflow-wrap: anywhere;
}
</style>
