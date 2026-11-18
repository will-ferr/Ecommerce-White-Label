<script setup lang="ts">
import type { NuxtError } from '#app'

const props = defineProps<{ error: NuxtError }>()

const notFound = computed(() => props.error.statusCode === 404)

useSeoMeta({ title: () => (notFound.value ? 'Página não encontrada' : 'Erro'), robots: 'noindex' })
</script>

<template>
  <main class="mx-auto flex min-h-dvh max-w-3xl flex-col justify-center gap-4 px-4 py-12 sm:px-6">
    <h1 class="font-display text-4xl text-ink">
      {{ notFound ? 'Página não encontrada' : 'Algo deu errado' }}
    </h1>
    <p class="text-muted">
      {{ notFound ? 'O endereço acessado não existe nesta loja.' : 'Não foi possível carregar esta página.' }}
    </p>
    <button class="w-fit cursor-pointer font-semibold text-primary underline underline-offset-4" type="button" @click="clearError({ redirect: '/' })">
      Voltar ao início
    </button>
  </main>
</template>
