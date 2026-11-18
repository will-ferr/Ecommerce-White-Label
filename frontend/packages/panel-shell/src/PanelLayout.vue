<script setup lang="ts">
import { UiAlert, UiButton } from '@whitelabel/ui'
import { RouterView } from 'vue-router'

import { useErrorStore } from './stores/errors'

defineProps<{ title: string }>()

const errors = useErrorStore()
</script>

<template>
  <div class="min-h-dvh">
    <header class="border-b border-line bg-surface">
      <div class="mx-auto flex min-h-16 max-w-3xl items-center px-4 sm:px-6">
        <p class="font-display text-xl text-ink">{{ title }}</p>
      </div>
    </header>

    <main class="mx-auto max-w-3xl px-4 py-8 sm:px-6 sm:py-12">
      <UiAlert v-if="errors.message" class="mb-6" :title="errors.message">
        <UiButton class="mt-2" @click="errors.dismiss()">Fechar</UiButton>
      </UiAlert>
      <RouterView />
    </main>
  </div>
</template>
