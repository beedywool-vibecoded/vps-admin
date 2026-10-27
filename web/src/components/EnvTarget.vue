<script setup lang="ts">
// Variables go to the app's environment or to a file in the deployed folder (some
// compose files read a real .env).
import M3Switch from './M3Switch.vue'

const file = defineModel<boolean>('file', { required: true })
const path = defineModel<string>('path', { required: true })
</script>

<template>
  <div class="target">
    <div class="row">
      <div class="grow">
        <div>Write a .env file</div>
        <div class="muted small">
          {{
            file
              ? 'The variables go into a file in the deployed folder instead of the app environment.'
              : 'The variables are handed to the app and to compose as environment variables.'
          }}
        </div>
      </div>
      <M3Switch v-model="file" label="Write a .env file" />
    </div>
    <div v-if="file" class="ffield">
      <label class="tf">
        <span>Path of the file</span>
        <input v-model.trim="path" placeholder=".env" spellcheck="false" autocomplete="off" />
      </label>
      <small>Relative to the app's folder, for example .env or config/.env. It is rewritten at each deployment and never committed.</small>
    </div>
  </div>
</template>

<style scoped>
.target {
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.row {
  display: flex;
  align-items: center;
  gap: 16px;
}
.ffield {
  display: flex;
  flex-direction: column;
  gap: 6px;
  min-width: 0;
}
.ffield small {
  padding-left: 16px;
  font-size: 12px;
  line-height: 16px;
  color: var(--on-sv);
}
.tf {
  box-sizing: border-box;
  display: flex;
  flex-direction: column;
  justify-content: center;
  gap: 2px;
  height: 56px;
  padding: 6px 16px;
  border-radius: 12px 12px 4px 4px;
  background: var(--schh);
  border-bottom: 1px solid var(--on-sv);
  overflow: hidden;
}
.tf:focus-within {
  border-bottom-color: var(--primary);
  box-shadow: inset 0 -1px 0 var(--primary);
}
.tf span {
  font-size: 12px;
  line-height: 16px;
  font-weight: 500;
  color: var(--primary);
}
.tf input {
  height: 24px;
  border: none;
  background: none;
  outline: none;
  font-size: 16px;
  color: var(--on-surface);
  padding: 0;
}
</style>
