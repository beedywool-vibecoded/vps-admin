<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import AppShell from './components/AppShell.vue'
import AppSnackbar from './components/AppSnackbar.vue'
import ConfirmDialog from './components/ConfirmDialog.vue'

const route = useRoute()
const ready = computed(() => route.matched.length > 0)
const bare = computed(() => route.meta.public === true)
</script>

<template>
  <RouterView v-if="ready && bare" />
  <AppShell v-else-if="ready">
    <RouterView />
  </AppShell>
  <AppSnackbar />
  <ConfirmDialog />
</template>
