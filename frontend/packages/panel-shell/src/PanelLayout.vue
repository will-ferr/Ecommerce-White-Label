<script setup lang="ts">
import { UiAlert, UiButton } from '@whitelabel/ui'
import { computed } from 'vue'
import { RouterLink, RouterView, useRouter } from 'vue-router'

import { useAuth, usePanel } from './context'
import { useErrorStore } from './stores/errors'

defineProps<{ title: string }>()

const errors = useErrorStore()
const auth = useAuth()
const panel = usePanel()
const router = useRouter()

const links = computed(() =>
  [
    { to: '/', label: 'Início' },
    ...panel.navigation,
    { to: '/equipe', label: 'Equipe', permission: 'team:read' },
    { to: '/auditoria', label: 'Auditoria', permission: 'audit:read' },
    { to: '/seguranca', label: 'Segurança' },
  ].filter((link) => !link.permission || auth.can(link.permission)),
)

async function signOut() {
  try {
    await auth.logout()
  } finally {
    await router.push({ name: 'login' })
  }
}
</script>

<template>
  <div class="min-h-dvh">
    <header class="border-b border-line bg-surface">
      <div class="mx-auto flex min-h-16 max-w-6xl flex-wrap items-center justify-between gap-x-6 gap-y-2 px-4 py-2 sm:px-6">
        <p class="font-display text-xl text-ink">{{ title }}</p>
        <nav v-if="auth.isAuthenticated" aria-label="Principal" class="flex flex-wrap items-center gap-x-4 gap-y-1">
          <RouterLink
            v-for="link in links"
            :key="link.to"
            :to="link.to"
            class="inline-flex min-h-11 items-center text-sm font-semibold text-muted hover:text-ink"
            :exact-active-class="link.to === '/' ? 'text-primary underline underline-offset-4' : ''"
            :active-class="link.to === '/' ? '' : 'text-primary underline underline-offset-4'"
          >
            {{ link.label }}
          </RouterLink>
          <button type="button" class="inline-flex min-h-11 cursor-pointer items-center text-sm font-semibold text-muted hover:text-ink" @click="signOut">
            Sair
          </button>
        </nav>
      </div>
    </header>

    <main class="mx-auto max-w-6xl px-4 py-8 sm:px-6 sm:py-12">
      <UiAlert v-if="errors.message" class="mb-6" :title="errors.message">
        <UiButton class="mt-2" @click="errors.dismiss()">Fechar</UiButton>
      </UiAlert>
      <RouterView />
    </main>
  </div>
</template>
