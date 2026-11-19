<script setup lang="ts">
import { ApiError } from '@whitelabel/api-client'
import { RichText } from '@whitelabel/ui'

const route = useRoute()
const api = useStoreApi()
const slug = computed(() => String(route.params.slug))

const { data: page, error } = await useAsyncData(() => `page-${slug.value}`, () => api.page(slug.value))

if (error.value) {
  const status = error.value instanceof ApiError ? error.value.status : (error.value as { statusCode?: number }).statusCode
  throw createError({ statusCode: status === 404 ? 404 : 503, statusMessage: status === 404 ? 'PAGE_NOT_FOUND' : 'UNAVAILABLE', fatal: true })
}

useSeoMeta({ title: () => page.value?.title, description: () => page.value?.seoDescription || undefined })
</script>

<template>
  <StoreLayout :wide="false">
    <article v-if="page" class="space-y-6">
      <h1 class="font-display text-4xl text-ink">{{ page.title }}</h1>
      <RichText :source="page.body" />
    </article>
  </StoreLayout>
</template>
