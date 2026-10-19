<script setup lang="ts">
// Picks the icon of a container group; shared with every admin and the app.
import { ref } from 'vue'
import M3Dialog from './M3Dialog.vue'
import Icon from './Icon.vue'
import { appearance } from '@/lib/appearance'
import { GROUP_ICONS, setGroupIcon } from '@/lib/groupIcons'
import { toastError } from '@/lib/ui'

const open = defineModel<boolean>({ required: true })
const props = defineProps<{ project: string }>()
const saving = ref(false)

async function pick(icon: string) {
  saving.value = true
  try {
    await setGroupIcon(props.project, icon)
    open.value = false
  } catch (e) {
    toastError(e)
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <M3Dialog v-model="open" :title="`Icon of ${project}`">
    <p class="muted small" style="margin: 0">Everyone sees it, on the panel and in the app.</p>
    <div class="grid" role="radiogroup" :aria-label="`Icon of ${project}`">
      <button
        v-for="name in GROUP_ICONS"
        :key="name"
        type="button"
        role="radio"
        class="opt st"
        :class="{ on: appearance.groupIcons?.[project] === name }"
        :aria-checked="appearance.groupIcons?.[project] === name"
        :aria-label="name"
        v-tip="name"
        :disabled="saving"
        @click="pick(name)"
      >
        <Icon :name="name" :size="22" />
      </button>
    </div>
    <template #actions>
      <button v-if="appearance.groupIcons?.[project]" class="btn btn-text st" type="button" :disabled="saving" @click="pick('')">Default shape</button>
      <button class="btn btn-text st" type="button" @click="open = false">Close</button>
    </template>
  </M3Dialog>
</template>

<style scoped>
.grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(48px, 1fr));
  gap: 8px;
}
.opt {
  aspect-ratio: 1;
  display: grid;
  place-items: center;
  border: none;
  border-radius: 14px;
  background: var(--sch);
  color: var(--on-surface);
  transition: border-radius 0.2s var(--ease);
}
.opt.on {
  background: var(--pc);
  color: var(--on-pc);
  border-radius: 24px;
}
</style>
