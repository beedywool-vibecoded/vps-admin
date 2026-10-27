<script setup lang="ts">
// Typing the server name guards against rebooting the wrong one; then wait for the agent.
import { ref } from 'vue'
import M3Dialog from './M3Dialog.vue'
import ShapeLoader from './ShapeLoader.vue'
import { api } from '@/lib/api'
import { session } from '@/lib/session'
import { toast, toastError } from '@/lib/ui'

const open = defineModel<boolean>({ required: true })
const typed = ref('')
const state = ref<'ask' | 'rebooting'>('ask')

async function reboot() {
  try {
    await api.reboot(typed.value.trim())
  } catch (e) {
    return toastError(e)
  }
  state.value = 'rebooting'
  await new Promise((r) => setTimeout(r, 15000))
  for (let i = 0; i < 90; i++) {
    try {
      const res = await fetch('/api/health', { cache: 'no-store' })
      if (res.ok) {
        toast('The server is back online')
        state.value = 'ask'
        typed.value = ''
        open.value = false
        return
      }
    } catch {
      // still down
    }
    await new Promise((r) => setTimeout(r, 4000))
  }
  toast('The server has not come back after 6 minutes. Check it from your hosting console.', true)
  state.value = 'ask'
}
</script>

<template>
  <M3Dialog v-model="open" title="Restart the server">
    <template v-if="state === 'ask'">
      <p class="muted" style="margin: 0">
        Every site, container and service stops for about a minute while the server reboots. Containers with a
        restart policy and enabled services come back on their own.
      </p>
      <label class="field">
        <input v-model="typed" :placeholder="`Type ${session.host?.hostname ?? 'the server name'} to confirm`" aria-label="Server name" autocomplete="off" spellcheck="false" />
      </label>
    </template>
    <p v-else class="muted" style="margin: 0">Rebooting… the panel reconnects as soon as the server answers.</p>
    <template #actions>
      <button class="btn btn-text st" type="button" :disabled="state === 'rebooting'" @click="open = false">Cancel</button>
      <button class="btn btn-danger st" type="button" :disabled="state === 'rebooting' || typed.trim() !== session.host?.hostname" :aria-busy="state === 'rebooting'" @click="reboot">
        <ShapeLoader v-if="state === 'rebooting'" small label="Rebooting" />
        {{ state === 'rebooting' ? 'Rebooting…' : 'Restart now' }}
      </button>
    </template>
  </M3Dialog>
</template>
