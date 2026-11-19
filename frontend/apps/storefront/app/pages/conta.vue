<script setup lang="ts">
import { AccountSecurity } from '@whitelabel/auth'
import { UiButton, UiSkeleton } from '@whitelabel/ui'

const tenant = useTenantStore()
const auth = useStoreAuth()
const ready = ref(false)

useSeoMeta({ title: 'Minha conta', robots: 'noindex,nofollow' })

onMounted(async () => {
  await auth.restore()
  if (!auth.isAuthenticated) {
    await navigateTo('/entrar', { replace: true })
    return
  }
  ready.value = true
})

async function signOut() {
  try {
    await auth.logout()
  } finally {
    await navigateTo('/')
  }
}
</script>

<template>
  <div v-if="tenant.config" class="flex min-h-dvh flex-col">
    <StoreHeader :name="tenant.config.store.name" :logo-url="tenant.config.theme.tokens.assets.logoUrl" :layout="tenant.config.theme.tokens.layout.header" />
    <main class="mx-auto w-full max-w-3xl flex-1 space-y-6 px-4 py-[var(--wl-space-section)] sm:px-6">
      <template v-if="ready && auth.user">
        <div class="flex flex-wrap items-start justify-between gap-4">
          <div class="min-w-0">
            <h1 class="font-display text-3xl text-ink sm:text-4xl">Olá, {{ auth.user.name }}</h1>
            <p class="mt-1 break-all text-muted">{{ auth.user.email }}</p>
          </div>
          <UiButton @click="signOut">Sair</UiButton>
        </div>
        <AccountSecurity :auth="auth" />
      </template>
      <UiSkeleton v-else class="h-40 w-full" />
    </main>
  </div>
</template>
