<script setup lang="ts">
import type { NuxtError } from '#app'

const props = defineProps<{ error: NuxtError }>()

const MESSAGES: Record<string, { title: string; text: string }> = {
  STORE_NOT_FOUND: {
    title: 'Loja não encontrada',
    text: 'Nenhuma loja está configurada neste endereço.',
  },
  STORE_UNAVAILABLE: {
    title: 'Loja indisponível',
    text: 'Esta loja está temporariamente fora do ar.',
  },
}

const message = computed(() => {
  const known = MESSAGES[props.error.statusMessage ?? '']
  if (known) return known
  if (props.error.statusCode === 404) {
    return { title: 'Página não encontrada', text: 'O endereço acessado não existe nesta loja.' }
  }
  return { title: 'Algo deu errado', text: 'Não foi possível carregar esta página. Tente novamente em instantes.' }
})

const tenant = useTenantStore()

useTenantTheme()
useSeoMeta({ title: () => message.value.title, robots: 'noindex,nofollow' })
</script>

<template>
  <main class="mx-auto flex min-h-dvh max-w-3xl flex-col justify-center gap-4 px-4 py-12 sm:px-6">
    <h1 class="font-display text-4xl text-ink">{{ message.title }}</h1>
    <p class="text-muted">{{ message.text }}</p>
    <button
      v-if="tenant.config"
      class="w-fit cursor-pointer font-semibold text-primary underline underline-offset-4"
      type="button"
      @click="clearError({ redirect: '/' })"
    >
      Voltar ao início
    </button>
  </main>
</template>
