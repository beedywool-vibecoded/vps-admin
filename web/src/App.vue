<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import AppShell from './components/AppShell.vue'
import AppSnackbar from './components/AppSnackbar.vue'
import ConfirmDialog from './components/ConfirmDialog.vue'

const route = useRoute()
const ready = computed(() => route.matched.length > 0)
const bare = computed(() => route.meta.public === true)
// Switching conversations keeps the assistant view mounted; other routes swap pages.
const pageKey = computed(() => (route.name === 'assistant' ? '/assistant' : route.path))
</script>

<template>
  <RouterView v-if="ready && bare" v-slot="{ Component }">
    <Transition name="page" mode="out-in">
      <component :is="Component" :key="pageKey" />
    </Transition>
  </RouterView>
  <AppShell v-else-if="ready">
    <RouterView v-slot="{ Component }">
      <Transition name="page" mode="out-in">
        <component :is="Component" :key="pageKey" />
      </Transition>
    </RouterView>
  </AppShell>
  <AppSnackbar />
  <ConfirmDialog />
</template>
