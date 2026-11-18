<script setup lang="ts">
export type BadgeTone = 'success' | 'warning' | 'danger'

defineProps<{ tone: BadgeTone }>()

const tones: Record<BadgeTone, string> = {
  success: 'bg-success-soft text-success',
  warning: 'bg-warning-soft text-warning',
  danger: 'bg-danger-soft text-danger',
}
</script>

<template>
  <span class="inline-flex items-center gap-2 rounded-full px-3 py-1 text-sm font-semibold" :class="tones[tone]">
    <span class="size-2 rounded-full bg-current" aria-hidden="true"></span>
    <slot />
  </span>
</template>
