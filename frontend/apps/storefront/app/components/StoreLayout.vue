<script setup lang="ts">
withDefaults(defineProps<{ wide?: boolean }>(), { wide: true })

const tenant = useTenantStore()
</script>

<template>
  <div v-if="tenant.config" class="flex min-h-dvh flex-col">
    <a href="#conteudo" class="sr-only focus:not-sr-only focus:absolute focus:left-4 focus:top-4 focus:z-10 focus:rounded-control focus:bg-surface focus:p-3">
      Pular para o conteúdo
    </a>
    <StoreHeader :name="tenant.config.store.name" :logo-url="tenant.config.theme.tokens.assets.logoUrl" :layout="tenant.config.theme.tokens.layout.header" />
    <main id="conteudo" class="mx-auto w-full flex-1 px-4 py-[var(--wl-space-section)] sm:px-6" :class="wide ? 'max-w-6xl' : 'max-w-3xl'">
      <slot />
    </main>
    <StoreFooter :name="tenant.config.store.name" />
  </div>
</template>
