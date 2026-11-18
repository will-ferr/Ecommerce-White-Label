<script setup lang="ts">
import { safeAssetUrl } from '@whitelabel/theme'

const props = defineProps<{ name: string; logoUrl: string; layout: 'classic' | 'centered' }>()

const logo = computed(() => safeAssetUrl(props.logoUrl))
</script>

<template>
  <header class="border-b border-line bg-surface">
    <div
      class="mx-auto flex min-h-16 max-w-5xl items-center px-4 py-3 sm:px-6"
      :class="layout === 'centered' ? 'justify-center' : 'justify-start'"
    >
      <NuxtLink to="/" class="font-display text-2xl text-ink">
        <img v-if="logo" :src="logo" :alt="name" class="h-10 w-auto" />
        <span v-else>{{ name }}</span>
      </NuxtLink>
    </div>
  </header>
</template>
