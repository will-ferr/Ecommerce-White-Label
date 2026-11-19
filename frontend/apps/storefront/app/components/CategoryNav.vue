<script setup lang="ts">
import type { Category } from '@whitelabel/api-client'

defineProps<{ categories: Category[]; current?: string }>()
</script>

<template>
  <nav v-if="categories.length" aria-label="Categorias">
    <ul class="flex flex-wrap gap-2">
      <li v-for="category in categories" :key="category.id">
        <NuxtLink
          :to="`/c/${category.slug}`"
          class="inline-flex min-h-11 items-center rounded-control border px-4 text-sm font-semibold"
          :class="category.slug === current ? 'border-primary bg-primary text-on-primary' : 'border-line bg-surface text-ink'"
          :aria-current="category.slug === current ? 'page' : undefined"
        >
          {{ category.name }}
        </NuxtLink>
      </li>
    </ul>
  </nav>
</template>
