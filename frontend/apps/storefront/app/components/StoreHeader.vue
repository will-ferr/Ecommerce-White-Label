<script setup lang="ts">
import { safeAssetUrl } from '@whitelabel/theme'

const props = defineProps<{ name: string; logoUrl: string; layout: 'classic' | 'centered' }>()

const logo = computed(() => safeAssetUrl(props.logoUrl))
const auth = useStoreAuth()
const cart = useCartStore()
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
  </header>
</template>
