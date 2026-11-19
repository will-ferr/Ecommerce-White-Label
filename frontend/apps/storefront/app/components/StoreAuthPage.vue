<script setup lang="ts">
defineProps<{ heading: string }>()

// Links in these pages may carry single-use tokens.
useHead({ meta: [{ name: 'referrer', content: 'no-referrer' }] })
useSeoMeta({ robots: 'noindex,nofollow' })
</script>

<template>
  <StoreLayout :wide="false">
    <div class="mx-auto max-w-md space-y-6">
      <h1 class="font-display text-3xl text-ink">{{ heading }}</h1>
      <div class="rounded-card border border-line bg-surface p-5 sm:p-6">
        <slot />
      </div>
      <slot name="links" />
    </div>
  </StoreLayout>
</template>
