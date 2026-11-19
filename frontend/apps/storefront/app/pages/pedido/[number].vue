<script setup lang="ts">
import type { Order } from '@whitelabel/api-client'
import { errorMessage } from '@whitelabel/auth'
import { UiAlert, UiButton, UiSkeleton } from '@whitelabel/ui'

const route = useRoute()
const api = useStoreApi()
const auth = useStoreAuth()

const number = Number(route.params.number)
const token = typeof route.query.token === 'string' ? route.query.token : undefined
const order = ref<Order>()
const failure = ref<string>()
const busy = ref(false)

// The link carries a private token, so it must not leak to other sites.
useHead({ meta: [{ name: 'referrer', content: 'no-referrer' }] })
useSeoMeta({ title: () => `Pedido ${number}`, robots: 'noindex,nofollow' })

onMounted(async () => {
  try {
    await auth.restore()
    order.value = await api.order(number, token)
  } catch (error) {
    failure.value = errorMessage(error)
  }
})

async function pay(approved: boolean) {
  busy.value = true
  failure.value = undefined
  try {
    order.value = await api.testPayment(number, token, approved)
  } catch (error) {
    failure.value = errorMessage(error)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <StoreLayout :wide="false">
    <div class="space-y-6">
      <h1 class="font-display text-4xl text-ink">Pedido {{ number }}</h1>
      <UiAlert v-if="failure" :title="failure" />
      <UiSkeleton v-if="!order && !failure" class="h-64 w-full" />
      <template v-if="order">
        <section v-if="order.testPayment" class="space-y-4 rounded-card border border-warning/40 bg-warning-soft p-5 text-warning" aria-labelledby="test-title">
          <h2 id="test-title" class="font-semibold">{{ order.payment?.label }}</h2>
          <p class="text-sm">Ambiente de demonstração: escolha o resultado do pagamento. Nenhum valor é cobrado.</p>
          <div class="flex flex-wrap gap-3">
            <UiButton :disabled="busy" @click="pay(true)">Aprovar pagamento de teste</UiButton>
            <button type="button" class="min-h-11 cursor-pointer px-2 font-semibold underline underline-offset-4" :disabled="busy" @click="pay(false)">Recusar pagamento</button>
          </div>
        </section>
        <OrderSummary :order="order" />
      </template>
    </div>
  </StoreLayout>
</template>
