<script setup lang="ts">
const tenant = useTenantStore()
const api = useStoreApi()

const { data } = await useAsyncData('home', async () => {
  const [categories, featured, latest] = await Promise.all([api.categories(), api.products({ featured: true, limit: 8 }), api.products()])
  return { categories, featured: featured.items, latest }
})

const centered = computed(() => tenant.config?.theme.tokens.layout.header === 'centered')
</script>

<template>
  <StoreLayout>
    <div v-if="tenant.config" class="space-y-[var(--wl-space-section)]">
      <section class="space-y-4" :class="centered ? 'text-center' : ''" aria-labelledby="store-title">
        <h1 id="store-title" class="font-display text-4xl leading-tight text-ink sm:text-6xl">{{ tenant.config.store.name }}</h1>
        <p v-if="tenant.config.seo.description" class="max-w-prose text-lg leading-relaxed text-muted" :class="centered ? 'mx-auto' : ''">
          {{ tenant.config.seo.description }}
        </p>
      </section>

      <CategoryNav v-if="data" :categories="data.categories" />

      <section v-if="data?.featured.length" class="space-y-6" aria-labelledby="featured-title">
        <h2 id="featured-title" class="font-display text-3xl text-ink">Destaques</h2>
        <ProductGrid :products="data.featured" />
      </section>

      <section class="space-y-6" aria-labelledby="all-title">
        <h2 id="all-title" class="font-display text-3xl text-ink">Todos os produtos</h2>
        <ProductListing v-if="data" :initial="data.latest.items" :next-cursor="data.latest.nextCursor" />
      </section>
    </div>
  </StoreLayout>
</template>
