<script setup lang="ts">
import { errorMessage } from '@whitelabel/auth'
import { UiAlert, UiSkeleton } from '@whitelabel/ui'

const auth = useStoreAuth()
const route = useRoute()

const state = ref<'loading' | 'done'>('loading')
const failure = ref<string>()

// The token is sent only after the page opens in a browser, so link scanners
// that fetch the address do not consume it.
onMounted(async () => {
  const token = typeof route.query.token === 'string' ? route.query.token : ''
  try {
    await auth.client().verifyEmail(token)
    state.value = 'done'
  } catch (error) {
    failure.value = errorMessage(error)
  }
})
</script>

<template>
  <StoreAuthPage heading="Confirmar e-mail">
    <UiAlert v-if="failure" :title="failure" />
    <p v-else-if="state === 'done'" role="status" class="text-ink">E-mail confirmado. Obrigado!</p>
    <UiSkeleton v-else class="h-6 w-full" />
    <template #links>
      <NuxtLink to="/entrar" class="inline-flex min-h-11 items-center font-semibold text-primary underline underline-offset-4">Ir para entrar</NuxtLink>
    </template>
  </StoreAuthPage>
</template>
