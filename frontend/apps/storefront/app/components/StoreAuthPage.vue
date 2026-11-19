<script setup lang="ts">
defineProps<{ heading: string }>()

const tenant = useTenantStore()

// Links in these pages may carry single-use tokens.
useHead({ meta: [{ name: 'referrer', content: 'no-referrer' }] })
useSeoMeta({ robots: 'noindex,nofollow' })
</script>

<template>
  <div v-if="tenant.config" class="flex min-h-dvh flex-col">
    <StoreHeader :name="tenant.config.store.name" :logo-url="tenant.config.theme.tokens.assets.logoUrl" :layout="tenant.config.theme.tokens.layout.header" />
    <main class="mx-auto w-full max-w-md flex-1 space-y-6 px-4 py-[var(--wl-space-section)]">
      <h1 class="font-display text-3xl text-ink">{{ heading }}</h1>
      <div class="rounded-card border border-line bg-surface p-5 sm:p-6">
        <slot />
      </div>
      <slot name="links" />
    </main>
  </div>
</template>
