<script setup lang="ts">
withDefaults(defineProps<{ type?: 'button' | 'submit'; disabled?: boolean }>(), {
  type: 'button',
  disabled: false,
})
</script>

<template>
  <button
    :type="type"
    :disabled="disabled"
    class="inline-flex min-h-11 cursor-pointer items-center justify-center rounded-control bg-primary px-4 text-sm font-semibold text-on-primary transition-colors hover:bg-primary-strong disabled:cursor-not-allowed disabled:opacity-60"
  >
    <slot />
  </button>
</template>
