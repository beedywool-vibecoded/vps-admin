<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import M3Shape from '@/components/M3Shape.vue'
import M3Switch from '@/components/M3Switch.vue'
import Icon from '@/components/Icon.vue'
import RebootDialog from '@/components/RebootDialog.vue'
import { api, type Check, type UpdateStatus } from '@/lib/api'
import { ago, dateTime } from '@/lib/format'
import { confirm, toast, toastError } from '@/lib/ui'

const st = ref<UpdateStatus | null>(null)
const packages = ref<Check | null>(null)
const reboot = ref<Check | null>(null)
const checking = ref(false)
const installing = ref(false)
const rebootOpen = ref(false)
const source = ref('')
const sourceSaving = ref(false)

const auto = computed({
  get: () => st.value?.auto ?? false,
  set: (v) => save({ auto: v }),
})
const pre = computed({
  get: () => st.value?.prerelease ?? false,
  set: (v) => save({ prerelease: v }),
})

async function save(s: { auto?: boolean; prerelease?: boolean }) {
  try {
    st.value = await api.updateSettings(s)
  } catch (e) {
    toastError(e)
  }
}

async function check() {
  checking.value = true
  try {
    st.value = await api.checkUpdate()
    toast(st.value.available ? `${st.value.latest?.tag} is available` : 'The agent is up to date')
  } catch (e) {
    toastError(e)
    // Keep showing the error the check recorded for this source.
    st.value = await api.update().catch(() => st.value)
  } finally {
    checking.value = false
  }
}

// Empty repo resets to the build's default repository.
async function setSource(repo: string) {
  sourceSaving.value = true
  try {
    st.value = await api.updateSettings({ repo })
    source.value = st.value.repo
    await check()
  } catch (e) {
    toastError(e)
  } finally {
    sourceSaving.value = false
  }
}

// After installing, the agent restarts; poll until the new version answers.
async function install() {
  const tag = st.value?.latest?.tag
  const ok = await confirm({
    title: `Install ${tag}?`,
    body: 'The agent checks the signature, swaps the binary and restarts. The panel reconnects by itself; if the new version fails to start, the previous one comes back automatically.',
    action: 'Install',
  })
  if (!ok) return
  installing.value = true
  try {
    await api.applyUpdate()
    for (let i = 0; i < 60; i++) {
      await new Promise((r) => setTimeout(r, 2000))
      try {
        const h = await fetch('/api/health').then((r) => r.json())
        if (h.version === tag) {
          toast(`Updated to ${tag}`)
          st.value = await api.update()
          return
        }
      } catch {
        // restarting
      }
    }
    toast('The agent did not come back with the new version. It may have rolled back, check the status below.', true)
    st.value = await api.update()
  } catch (e) {
    toastError(e)
  } finally {
    installing.value = false
  }
}

onMounted(async () => {
  try {
    st.value = await api.update()
    source.value = st.value.repo
    const sec = await api.security()
    packages.value = sec.checks.find((c) => c.id === 'updates') ?? null
    reboot.value = sec.checks.find((c) => c.id === 'reboot') ?? null
  } catch (e) {
    toastError(e)
  }
})
</script>

<template>
  <div class="page">
    <div class="page-title">
      <h1>Updates</h1>
    </div>

    <section class="card stack">
      <div class="head">
        <M3Shape kind="cookie" :size="56" fill="var(--pc)" />
        <div class="grow">
          <h2>VPS Admin agent</h2>
          <div class="muted small">
            Version <strong>{{ st?.current ?? '…' }}</strong> ·
            {{ st?.checkedAt ? `checked ${ago(st.checkedAt)}` : 'never checked' }}
          </div>
        </div>
        <button class="btn btn-tonal st" type="button" :disabled="checking || installing" @click="check">
          {{ checking ? 'Checking…' : 'Check now' }}
        </button>
        <button v-if="st?.available" class="btn btn-filled st" type="button" :disabled="installing" @click="install">
          {{ installing ? 'Installing…' : `Install ${st.latest?.tag}` }}
        </button>
      </div>

      <div v-if="st?.error" class="note bad"><Icon name="alert" :size="18" /> {{ st.error }}</div>
      <div v-if="st?.lastResult" class="note" :class="{ bad: st.lastResult.startsWith('rolled back') }">
        <Icon :name="st.lastResult.startsWith('rolled back') ? 'alert' : 'history'" :size="18" />
        Last update: {{ st.lastResult }}{{ st.lastResult.startsWith('rolled back') ? ' (it failed to start, the previous version was restored and this one will not be retried automatically)' : '' }}
      </div>

      <div v-if="st?.latest" class="release">
        <div class="rel-head">
          <strong>{{ st.latest.tag }}</strong>
          <span v-if="st.latest.prerelease" class="chip">pre-release</span>
          <span class="muted small">{{ dateTime(st.latest.published) }}</span>
          <a :href="st.latest.url" target="_blank" rel="noopener" class="btn btn-text st">On GitHub <Icon name="external" :size="16" /></a>
        </div>
        <pre class="notes">{{ st.latest.notes || 'No release notes.' }}</pre>
      </div>

      <div class="setting source">
        <div class="grow">
          <div>Update source</div>
          <div class="muted small">
            Releases are read from this GitHub repository. Use <code>{{ st?.defaultRepo ?? '…' }}</code> for the official
            builds, or a link to your own fork. Only builds signed with the project key are installed.
          </div>
        </div>
        <form class="source-form" @submit.prevent="setSource(source.trim())">
          <label class="field">
            <Icon name="github" :size="20" />
            <input
              v-model="source"
              placeholder="owner/name or https://github.com/owner/name"
              aria-label="Update source repository"
              spellcheck="false"
              autocomplete="off"
            />
          </label>
          <button
            class="btn btn-tonal st"
            type="submit"
            :disabled="sourceSaving || !source.trim() || source.trim() === st?.repo"
          >
            {{ sourceSaving ? 'Saving…' : 'Use this source' }}
          </button>
          <button
            v-if="st && st.repo !== st.defaultRepo"
            class="btn btn-text st"
            type="button"
            :disabled="sourceSaving"
            @click="setSource('')"
          >
            Reset to the default
          </button>
        </form>
      </div>
      <div class="setting">
        <div class="grow">
          <div>Install updates automatically</div>
          <div class="muted small">Checks the update source every 6 hours. Only builds signed with the project key are installed.</div>
        </div>
        <M3Switch v-model="auto" label="Install updates automatically" :disabled="!st" />
      </div>
      <div class="setting">
        <div class="grow">
          <div>Follow pre-releases</div>
          <div class="muted small">Get release candidates before they are marked stable.</div>
        </div>
        <M3Switch v-model="pre" label="Follow pre-releases" :disabled="!st" />
      </div>
    </section>

    <section class="card stack">
      <div class="head">
        <M3Shape kind="clover" :size="56" fill="var(--tc)" />
        <div class="grow">
          <h2>System packages</h2>
          <div class="muted small">{{ packages?.detail ?? 'Checking…' }}</div>
        </div>
        <button class="btn st" :class="reboot && reboot.status !== 'ok' ? 'btn-filled' : 'btn-outlined'" type="button" @click="rebootOpen = true">
          <Icon name="power" :size="18" /> Restart the server
        </button>
      </div>
      <div v-if="reboot && reboot.status !== 'ok'" class="note"><Icon name="info" :size="18" /> {{ reboot.detail }}</div>
      <p class="muted small" style="margin: 0">
        Installing packages and rebooting from the panel comes with the assistant, behind an approval for each
        step. Until then, use <code>sudo apt full-upgrade</code> (or your distribution's equivalent) over SSH.
      </p>
    </section>
    <RebootDialog v-model="rebootOpen" />
  </div>
</template>

<style scoped>
.stack {
  display: flex;
  flex-direction: column;
  gap: 16px;
}
.head {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 16px;
}
.head h2 {
  font-size: 22px;
}
.note {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  padding: 12px 14px;
  border-radius: 14px;
  background: var(--sch);
  font-size: 14px;
}
.note.bad {
  background: var(--ec);
  color: var(--on-ec);
}
.note svg {
  flex-shrink: 0;
  margin-top: 1px;
}
.release {
  border-radius: 18px;
  background: var(--sch);
  padding: 14px 16px;
}
.rel-head {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 10px;
}
.rel-head .btn-text {
  margin-left: auto;
}
.notes {
  margin: 10px 0 0;
  max-height: 260px;
  overflow: auto;
  font-family: var(--font);
  font-size: 14px;
  white-space: pre-wrap;
  color: var(--on-sv);
}
.setting {
  display: flex;
  align-items: center;
  gap: 16px;
  padding-top: 14px;
  border-top: 1px solid var(--sch);
}
.source {
  flex-wrap: wrap;
}
.source-form {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
  width: 100%;
}
.source-form .field {
  flex: 1 1 280px;
  min-width: 220px;
}
</style>
