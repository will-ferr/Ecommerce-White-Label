<script setup lang="ts">
import { ApiError } from '@whitelabel/api-client'

const route = useRoute()
const api = useStoreApi()
const slug = computed(() => String(route.params.slug))

const { data, error } = await useAsyncData(
  () => `category-${slug.value}`,
  async () => {
    const [categories, page] = await Promise.all([api.categories(), api.products({ category: slug.value })])
    return { categories, page, category: categories.find((category) => category.slug === slug.value) }
  },
)

if (error.value) {
  const status = error.value instanceof ApiError ? error.value.status : (error.value as { statusCode?: number }).statusCode
  throw createError({ statusCode: status === 404 ? 404 : 503, statusMessage: status === 404 ? 'PAGE_NOT_FOUND' : 'UNAVAILABLE', fatal: true })
}

useSeoMeta({
  title: () => data.value?.category?.name,
  description: () => data.value?.category?.description || undefined,
})
</script>

<template>
  <StoreLayout>
    <div v-if="data" class="space-y-8">
      <div class="space-y-3">
        <h1 class="font-display text-4xl text-ink">{{ data.category?.name }}</h1>
        <p v-if="data.category?.description" class="max-w-prose text-muted">{{ data.category.description }}</p>
      </div>
      <CategoryNav :categories="data.categories" :current="slug" />
      <ProductListing :initial="data.page.items" :next-cursor="data.page.nextCursor" :category="slug" />
    </div>
  </StoreLayout>
</template>
