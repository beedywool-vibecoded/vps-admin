<script setup lang="ts">
import { appearance } from '@/lib/appearance'
</script>

<template>
  <div v-if="appearance.background" class="server-bg" aria-hidden="true">
    <img :src="`/api/appearance/background?v=${appearance.version}`" alt="" :style="{ opacity: appearance.opacity }" />
  </div>
</template>

<style scoped>
.server-bg {
  position: fixed;
  inset: 0;
  z-index: -1;
  pointer-events: none;
}
/* A dark tint over the picture so the cards stand out from it: stronger in light mode, where the
   cards are light too */
.server-bg::after {
  content: '';
  position: absolute;
  inset: 0;
  background: rgb(0 0 0 / 0.2);
}
:global([data-theme='dark']) .server-bg::after {
  background: rgb(0 0 0 / 0.3);
}
.server-bg img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  transition: opacity 0.2s;
}
</style>
