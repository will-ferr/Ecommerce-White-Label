<script setup lang="ts">
const route = useRoute()
const api = useStoreApi()
const query = computed(() => (typeof route.query.q === 'string' ? route.query.q.trim().slice(0, 100) : ''))

const { data, error } = await useAsyncData(
  () => `search-${query.value}`,
  async () => (query.value ? (await api.products({ q: query.value })).items : []),
  { watch: [query] },
)

if (error.value) {
  throw createError({ statusCode: 503, statusMessage: 'UNAVAILABLE', fatal: true })
}

// Result pages change with every query and add nothing to search engines.
useSeoMeta({ title: () => (query.value ? `Busca: ${query.value}` : 'Busca'), robots: 'noindex,follow' })
</script>

<template>
  <StoreLayout>
    <div class="space-y-6">
      <h1 class="font-display text-4xl text-ink">{{ query ? `Resultados para “${query}”` : 'Buscar produtos' }}</h1>
      <p v-if="!query" class="text-muted">Digite o que você procura no campo de busca.</p>
      <template v-else-if="data">
        <p class="text-muted" role="status">
          {{ data.length === 0 ? 'Nenhum produto encontrado. Tente outras palavras.' : `${data.length} ${data.length === 1 ? 'produto encontrado' : 'produtos encontrados'}` }}
        </p>
        <ProductGrid v-if="data.length" :products="data" />
      </template>
    </div>
  </StoreLayout>
</template>
