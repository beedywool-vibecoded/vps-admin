<script setup lang="ts">
// Overrides of the generated build: commands for Node and static sites, file paths for
// Dockerfile and Compose. Empty fields keep the defaults shown as placeholders.
import { computed } from 'vue'
import M3Expand from './M3Expand.vue'

export interface Commands {
  installCmd: string
  buildCmd: string
  startCmd: string
  dockerfile: string
  composeFile: string
}

const model = defineModel<Commands>({ required: true })
const props = defineProps<{ build: string }>()

const custom = computed(() => Object.values(model.value).some(Boolean))
const node = computed(() => props.build === 'node' || props.build === 'static')
</script>

<template>
  <M3Expand class="cmds" :initially-open="custom">
    <template #summary>
      <div class="grow">
        <div>Custom commands</div>
        <div class="muted small">{{ custom ? 'Some steps are customized' : 'Optional: replace the default install, build and start steps' }}</div>
      </div>
    </template>
    <div class="grid">
      <template v-if="node || !build">
        <label class="tf">
          <span>Install</span>
          <input v-model.trim="model.installCmd" placeholder="npm ci" spellcheck="false" />
        </label>
        <label class="tf">
          <span>Build</span>
          <input v-model.trim="model.buildCmd" placeholder="npm run build" spellcheck="false" />
        </label>
        <label v-if="build !== 'static'" class="tf">
          <span>Start</span>
          <input v-model.trim="model.startCmd" placeholder="npm start" spellcheck="false" />
          <small>Runs with <code>sh -c</code>, <code>PORT=3000</code> is set</small>
        </label>
      </template>
      <label v-if="build === 'dockerfile' || !build" class="tf">
        <span>Dockerfile</span>
        <input v-model.trim="model.dockerfile" placeholder="Dockerfile" spellcheck="false" />
        <small>Relative to the root folder</small>
      </label>
      <label v-if="build === 'compose' || !build" class="tf">
        <span>Compose file</span>
        <input v-model.trim="model.composeFile" placeholder="compose.yaml" spellcheck="false" />
        <small>Relative to the root folder</small>
      </label>
    </div>
  </M3Expand>
</template>

<style scoped>
.cmds {
  border: 1px solid var(--outline-v);
  border-radius: 16px;
  padding: 6px;
}
.cmds :deep(.head) {
  padding: 8px 10px;
}
.grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
  gap: 12px;
  padding: 10px 10px 8px;
}
.tf {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 10px 16px 8px;
  border-radius: 12px 12px 4px 4px;
  background: var(--schh);
  border-bottom: 1px solid var(--on-sv);
}
.tf:focus-within {
  border-bottom-color: var(--primary);
  box-shadow: inset 0 -1px 0 var(--primary);
}
.tf span {
  font-size: 12px;
  font-weight: 500;
  color: var(--primary);
}
.tf input {
  border: none;
  background: none;
  outline: none;
  padding: 2px 0;
  color: var(--on-surface);
  font-family: var(--mono);
  font-size: 13px;
}
.tf small {
  font-size: 11px;
  color: var(--on-sv);
}
</style>
