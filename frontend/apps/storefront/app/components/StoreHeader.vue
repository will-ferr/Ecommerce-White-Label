<script setup lang="ts">
import { safeAssetUrl } from '@whitelabel/theme'

const props = defineProps<{ name: string; logoUrl: string; layout: 'classic' | 'centered' }>()

const logo = computed(() => safeAssetUrl(props.logoUrl))
const auth = useStoreAuth()
</script>

<template>
  <header class="border-b border-line bg-surface">
    <div
      class="relative mx-auto flex min-h-16 max-w-5xl items-center px-4 py-3 sm:px-6"
      :class="layout === 'centered' ? 'justify-center' : 'justify-start'"
    >
      <NuxtLink to="/" class="max-w-[60%] truncate font-display text-2xl text-ink">
        <img v-if="logo" :src="logo" :alt="name" class="h-10 w-auto" />
        <span v-else>{{ name }}</span>
      </NuxtLink>
      <NuxtLink
        :to="auth.isAuthenticated ? '/conta' : '/entrar'"
        class="absolute right-4 inline-flex min-h-11 items-center text-sm font-semibold text-primary underline underline-offset-4 sm:right-6"
      >
        {{ auth.isAuthenticated ? 'Minha conta' : 'Entrar' }}
      </NuxtLink>
    </div>
  </header>
</template>
