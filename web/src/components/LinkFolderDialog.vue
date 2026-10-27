<script setup lang="ts">
// Ties an existing compose folder to its GitHub repo; pushes then fast-forward it and
// run docker compose up.
import { computed, ref, watch } from 'vue'
import M3Dialog from './M3Dialog.vue'
import M3Select from './M3Select.vue'
import M3Switch from './M3Switch.vue'
import { api, type Repo } from '@/lib/api'
import { toast, toastError } from '@/lib/ui'

const open = defineModel<boolean>({ required: true })
const props = defineProps<{ project: string; dir?: string }>()
const emit = defineEmits<{ linked: [] }>()

const repos = ref<Repo[]>([])
const repo = ref('')
const branch = ref('')
const autoDeploy = ref(true)
const problem = ref('')
const loading = ref(false)
const saving = ref(false)
const options = computed(() => repos.value.map((r) => ({ value: r.fullName, label: r.fullName, hint: r.private ? 'private' : undefined })))

watch(open, async (v) => {
  if (!v) return
  loading.value = true
  problem.value = ''
  try {
    const [gh, info] = await Promise.all([api.repos().then((r) => r.repos), api.inspectFolder(props.project)])
    repos.value = gh
    repo.value = gh.find((r) => r.fullName.toLowerCase() === info.folder.repo.toLowerCase())?.fullName ?? ''
    branch.value = info.folder.branch || 'main'
    if (info.folder.repo && !repo.value) problem.value = `The folder follows ${info.folder.repo}, which the GitHub App cannot read yet: add it on GitHub (Apps, Choose repositories).`
  } catch (e) {
    problem.value = e instanceof Error ? e.message : String(e)
  } finally {
    loading.value = false
  }
})

async function link() {
  saving.value = true
  try {
    await api.linkFolder({ project: props.project, repo: repo.value, branch: branch.value.trim(), rootDir: '', autoDeploy: autoDeploy.value })
    toast(`${props.project} now updates from ${repo.value}@${branch.value}`)
    open.value = false
    emit('linked')
  } catch (e) {
    toastError(e)
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <M3Dialog v-model="open" :title="`Update ${project} from GitHub`">
    <p class="muted small" style="margin: 0">
      On every push to the branch (or when you press Update), the folder <code>{{ dir }}</code> fast-forwards to the new
      commit and <code>docker compose up -d --build</code> runs. Local changes are never overwritten: the update stops instead.
    </p>
    <p v-if="loading" class="muted">Reading the folder…</p>
    <div v-if="problem" class="note small">{{ problem }}</div>
    <template v-if="!loading">
      <M3Select v-model="repo" label="Repository" :options="options" />
      <label class="field"><input v-model="branch" placeholder="Branch" aria-label="Branch" spellcheck="false" /></label>
      <div class="row-s">
        <div class="grow"><div>Update on every push</div><div class="muted small">Otherwise only with the Update button</div></div>
        <M3Switch v-model="autoDeploy" label="Update on every push" />
      </div>
    </template>
    <template #actions>
      <button class="btn btn-text st" type="button" @click="open = false">Cancel</button>
      <button class="btn btn-filled st" type="button" :disabled="saving || !repo || !branch.trim()" @click="link">Link</button>
    </template>
  </M3Dialog>
</template>

<style scoped>
.note {
  padding: 10px 12px;
  border-radius: 12px;
  background: var(--sch);
}
.row-s {
  display: flex;
  align-items: center;
  gap: 16px;
}
</style>
