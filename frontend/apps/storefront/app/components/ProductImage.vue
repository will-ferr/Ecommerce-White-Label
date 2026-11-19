<script setup lang="ts">
import type { Image } from '@whitelabel/api-client'
import { safeAssetUrl } from '@whitelabel/theme'

const props = defineProps<{ image: Image | null | undefined; name: string }>()

const source = computed(() => (props.image ? safeAssetUrl(props.image.url) : undefined))
const initial = computed(() => props.name.trim().charAt(0).toUpperCase())
</script>

<template>
  <img
    v-if="source"
    :src="source"
    :alt="image?.alt || name"
    loading="lazy"
    class="aspect-square w-full rounded-card object-cover"
  />
  <div v-else class="flex aspect-square w-full items-center justify-center rounded-card bg-primary" role="img" :aria-label="name">
    <span class="font-display text-5xl text-on-primary" aria-hidden="true">{{ initial }}</span>
  </div>
</template>
