<script setup lang="ts">
import { safeAssetUrl } from '@whitelabel/theme'

const props = defineProps<{ name: string; logoUrl: string; layout: 'classic' | 'centered' }>()

const logo = computed(() => safeAssetUrl(props.logoUrl))
const auth = useStoreAuth()
const cart = useCartStore()
const route = useRoute()
const searchId = useId()
const query = ref(typeof route.query.q === 'string' ? route.query.q : '')

watch(() => route.query.q, (value) => {
  query.value = typeof value === 'string' ? value : ''
})

async function search() {
  const text = query.value.trim()
  if (text) await navigateTo({ path: '/busca', query: { q: text } })
}
</script>

<template>
  <header class="border-b border-line bg-surface">
    <div
      class="relative mx-auto flex min-h-16 max-w-6xl items-center justify-between gap-4 px-4 py-3 sm:px-6"
      :class="layout === 'centered' ? 'sm:justify-center' : 'sm:justify-start'"
    >
      <NuxtLink to="/" class="min-w-0 truncate font-display text-xl text-ink sm:max-w-[50%] sm:text-2xl">
        <img v-if="logo" :src="logo" :alt="name" class="h-10 w-auto" />
        <span v-else>{{ name }}</span>
      </NuxtLink>
      <nav aria-label="Conta e carrinho" class="flex shrink-0 items-center gap-4 sm:absolute sm:right-6">
        <NuxtLink :to="auth.isAuthenticated ? '/conta' : '/entrar'" class="inline-flex min-h-11 items-center text-sm font-semibold text-primary underline underline-offset-4">
          {{ auth.isAuthenticated ? 'Minha conta' : 'Entrar' }}
        </NuxtLink>
        <NuxtLink to="/carrinho" class="inline-flex min-h-11 items-center gap-2 text-sm font-semibold text-primary underline underline-offset-4">
          Carrinho
          <span class="rounded-full bg-primary px-2 py-0.5 text-xs text-on-primary no-underline" data-testid="cart-count">
            <span class="sr-only">com </span>{{ cart.count }}<span class="sr-only"> itens</span>
          </span>
        </NuxtLink>
      </nav>
    </div>
    <form role="search" action="/busca" method="get" class="mx-auto flex max-w-6xl gap-2 px-4 pb-3 sm:px-6" @submit.prevent="search">
      <label :for="searchId" class="sr-only">Buscar produtos</label>
      <input
        :id="searchId" v-model="query" name="q" type="search" maxlength="100" placeholder="Buscar produtos"
        class="block min-h-11 w-full min-w-0 rounded-control border border-muted bg-surface px-3 text-ink"
      />
      <button type="submit" class="min-h-11 shrink-0 rounded-control bg-primary px-4 font-semibold text-on-primary">Buscar</button>
    </form>
  </header>
</template>
