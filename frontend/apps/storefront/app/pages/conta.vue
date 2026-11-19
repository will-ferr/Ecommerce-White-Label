<script setup lang="ts">
import { AccountSecurity } from '@whitelabel/auth'
import { UiButton, UiSkeleton } from '@whitelabel/ui'

const auth = useStoreAuth()
const cart = useCartStore()
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

// The server already ended every session; only local state remains.
async function afterErasure() {
  await auth.logout().catch(() => undefined)
  await cart.load().catch(() => undefined)
  await navigateTo('/')
}

async function signOut() {
  try {
    await auth.logout()
  } finally {
    await cart.load().catch(() => undefined)
    await navigateTo('/')
  }
}
</script>

<template>
  <StoreLayout :wide="false">
    <div class="space-y-6">
      <template v-if="ready && auth.user">
        <div class="flex flex-wrap items-start justify-between gap-4">
          <div class="min-w-0">
            <h1 class="font-display text-3xl text-ink sm:text-4xl">Olá, {{ auth.user.name }}</h1>
            <p class="mt-1 break-all text-muted">{{ auth.user.email }}</p>
          </div>
          <UiButton @click="signOut">Sair</UiButton>
        </div>
        <AccountOrders />
        <AccountSecurity :auth="auth" />
        <AccountPrivacy @erased="afterErasure" />
      </template>
      <UiSkeleton v-else class="h-40 w-full" />
    </div>
  </StoreLayout>
</template>
