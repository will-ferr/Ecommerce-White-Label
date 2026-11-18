<script setup lang="ts">
defineProps<{ title: string }>()
</script>

<template>
  <div role="alert" class="rounded-control border border-danger/30 bg-danger-soft px-4 py-3 text-sm text-danger">
    <p class="font-semibold">{{ title }}</p>
    <div v-if="$slots.default" class="mt-1"><slot /></div>
  </div>
</template>
