<script setup lang="ts">
const tenant = useTenantStore()

const FEATURE_LABELS: Record<string, string> = {
  reviews: 'Avaliações',
  wishlist: 'Favoritos',
  loyalty: 'Fidelidade',
  subscriptions: 'Assinaturas',
  marketplace: 'Marketplace',
}

const config = computed(() => tenant.config)
const activeFeatures = computed(() =>
  Object.entries(config.value?.features ?? {})
    .filter(([, enabled]) => enabled)
    .map(([key]) => FEATURE_LABELS[key] ?? key),
)
const centered = computed(() => config.value?.theme.tokens.layout.header === 'centered')
</script>

<template>
  <div v-if="config" class="flex min-h-dvh flex-col">
    <StoreHeader :name="config.store.name" :logo-url="config.theme.tokens.assets.logoUrl" :layout="config.theme.tokens.layout.header" />

    <main class="mx-auto w-full max-w-5xl flex-1 px-4 py-[var(--wl-space-section)] sm:px-6">
      <div class="space-y-5" :class="centered ? 'text-center' : ''">
        <h1 class="font-display text-4xl leading-tight text-ink sm:text-6xl">{{ config.store.name }}</h1>
        <p v-if="config.seo.description" class="max-w-prose text-lg leading-relaxed text-muted" :class="centered ? 'mx-auto' : ''">
          {{ config.seo.description }}
        </p>
      </div>

      <section class="mt-[var(--wl-space-section)] rounded-card border border-line bg-surface p-5 sm:p-8" aria-labelledby="catalog-title">
        <h2 id="catalog-title" class="font-display text-2xl text-ink">Catálogo em preparação</h2>
        <p class="mt-2 text-muted">Os produtos desta loja serão publicados em breve.</p>

        <div v-if="activeFeatures.length" class="mt-6">
          <h3 class="text-sm font-semibold text-muted">Recursos ativos nesta loja</h3>
          <ul class="mt-3 flex flex-wrap gap-2">
            <li
              v-for="feature in activeFeatures"
              :key="feature"
              class="rounded-control bg-primary px-3 py-1.5 text-sm font-semibold text-on-primary"
            >
              {{ feature }}
            </li>
          </ul>
        </div>
      </section>
    </main>
  </div>
</template>
