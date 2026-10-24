<script setup lang="ts">
// Points an existing app at another repository, for example one that moved from a user
// to an organization. The App's installation follows the repository automatically.
import { computed, ref, watch } from 'vue'
import M3Dialog from './M3Dialog.vue'
import M3Select from './M3Select.vue'
import Icon from './Icon.vue'
import ShapeLoader from './ShapeLoader.vue'
import { api, type Installation, type Repo } from '@/lib/api'
import { ago } from '@/lib/format'

const open = defineModel<boolean>({ required: true })
const props = defineProps<{ repo: string; branch: string }>()
const emit = defineEmits<{ change: [source: { repo: string; branch: string }] }>()

const repos = ref<Repo[]>([])
const installations = ref<Installation[]>([])
const query = ref('')
const account = ref('all')
const picked = ref<Repo | null>(null)
const branch = ref('')
const branches = ref<string[]>([])
const loading = ref(false)
const problem = ref('')

const accountOptions = computed(() => [
  { value: 'all', label: 'All accounts' },
  ...installations.value.map((i) => ({
    value: String(i.id),
    label: i.account,
    hint: i.type === 'Organization' ? 'Organization' : 'Personal account',
  })),
])
const shown = computed(() => {
  const q = query.value.trim().toLowerCase()
  return repos.value
    .filter((r) => (account.value === 'all' || String(r.installationId) === account.value) && (!q || r.fullName.toLowerCase().includes(q)))
    .sort((a, b) => b.pushedAt.localeCompare(a.pushedAt))
})
const branchOptions = computed(() => [...new Set([branch.value, ...branches.value].filter(Boolean))].map((b) => ({ value: b, label: b })))
// A transferred repository keeps its commits but changes owner: the stored name then
// no longer appears in what the App can read.
const missing = computed(() => !!props.repo && !repos.value.some((r) => r.fullName.toLowerCase() === props.repo.toLowerCase()))

function loadBranches(r: Repo, keep: string) {
  branches.value = []
  api
    .branches(r.installationId, r.fullName)
    .then((res) => {
      if (picked.value?.fullName !== r.fullName) return
      branches.value = res.branches
      if (keep && res.branches.includes(keep)) branch.value = keep
      else if (res.branches.length && !res.branches.includes(branch.value)) branch.value = res.branches[0]!
    })
    .catch(() => {})
}

function pick(r: Repo) {
  picked.value = r
  branch.value = r.defaultBranch
  loadBranches(r, r.defaultBranch)
}

watch(open, async (v) => {
  if (!v) return
  loading.value = true
  problem.value = ''
  query.value = ''
  account.value = 'all'
  picked.value = null
  branches.value = []
  try {
    const [gh, list] = await Promise.all([api.github(), api.repos()])
    installations.value = gh.installations ?? []
    repos.value = list.repos
    const cur = list.repos.find((r) => r.fullName.toLowerCase() === props.repo.toLowerCase())
    if (cur) {
      picked.value = cur
      branch.value = props.branch || cur.defaultBranch
      loadBranches(cur, branch.value)
    } else {
      branch.value = ''
    }
  } catch (e) {
    problem.value = e instanceof Error ? e.message : String(e)
  } finally {
    loading.value = false
  }
})

function change() {
  if (!picked.value || !branch.value.trim()) return
  emit('change', { repo: picked.value.fullName, branch: branch.value.trim() })
}
</script>

<template>
  <M3Dialog v-model="open" title="Change source">
    <div class="src-body">
      <p class="muted small note">
        The app keeps its settings and deployment history. Pushes and deployments follow the repository picked here,
        including when it was moved to another account or organization.
      </p>
      <div v-if="loading" class="loading-block"><ShapeLoader label="Loading repositories" /></div>
      <template v-else>
        <div v-if="problem" class="note small">{{ problem }}</div>
        <div v-else-if="missing" class="note small">
          The current repository <code>{{ repo }}</code> is not readable by the GitHub App anymore. Pick it under its new
          account, or add that account on GitHub first.
        </div>
        <div class="filters">
          <label class="field search">
            <Icon name="search" :size="20" />
            <input v-model="query" placeholder="Search repositories" aria-label="Search repositories" />
          </label>
          <M3Select v-if="accountOptions.length > 2" v-model="account" class="account" label="Account" :options="accountOptions" />
        </div>
        <div class="repos" role="listbox" aria-label="Repositories">
          <button
            v-for="r in shown"
            :key="r.fullName"
            type="button"
            role="option"
            class="repo st"
            :class="{ sel: picked?.fullName === r.fullName }"
            :aria-selected="picked?.fullName === r.fullName"
            @click="pick(r)"
          >
            <span class="grow">
              <span class="rname">{{ r.fullName }}</span>
              <span class="muted small">
                {{ r.language || 'Repository' }} · pushed {{ ago(r.pushedAt) }}
              </span>
            </span>
            <Icon v-if="r.private" name="lock" :size="16" />
          </button>
        </div>
        <p v-if="!shown.length" class="muted small empty">No repository matches.</p>
        <M3Select v-if="picked" v-model="branch" label="Branch" :options="branchOptions" />
      </template>
    </div>
    <template #actions>
      <button class="btn btn-text st" type="button" @click="open = false">Cancel</button>
      <button class="btn btn-filled st" type="button" :disabled="loading || !picked || !branch.trim()" @click="change">Change source</button>
    </template>
  </M3Dialog>
</template>

<style scoped>
.src-body {
  display: flex;
  flex-direction: column;
  gap: 12px;
  max-height: calc(100vh - 250px);
  overflow: auto;
}
.note {
  margin: 0;
}
.note code {
  overflow-wrap: anywhere;
}
.filters {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 10px;
}
.filters .search {
  flex: 1;
  min-width: 0;
}
.account {
  flex: none;
  width: 190px;
}
.repos {
  display: flex;
  flex-direction: column;
  gap: 4px;
  max-height: 280px;
  overflow: auto;
}
.repo {
  flex-shrink: 0;
  display: flex;
  align-items: center;
  gap: 12px;
  min-height: 60px;
  padding: 10px 14px;
  border-radius: 14px;
  border: 2px solid transparent;
  background: transparent;
  color: var(--on-surface);
  text-align: left;
}
.repo .grow {
  display: flex;
  flex-direction: column;
  min-width: 0;
}
.repo.sel {
  background: var(--pc);
  border-color: var(--primary);
  color: var(--on-pc);
}
.rname {
  overflow-wrap: anywhere;
  font-size: 15px;
  font-weight: 500;
}
.empty {
  margin: 0;
}
</style>
