<script setup lang="ts">
import type { Order } from '@whitelabel/api-client'
import { errorMessage } from '@whitelabel/auth'
import { orderStatusLabel, orderStatusTone, UiAlert, UiBadge, UiSkeleton } from '@whitelabel/ui'

const api = useStoreApi()
const money = useMoney()
const orders = ref<Order[]>()
const failure = ref<string>()

onMounted(async () => {
  try {
    orders.value = (await api.accountOrders()).items
  } catch (error) {
    failure.value = errorMessage(error)
  }
})
</script>

<template>
  <section class="space-y-4 rounded-card border border-line bg-surface p-5 sm:p-6" aria-labelledby="orders-title">
    <h2 id="orders-title" class="font-display text-2xl text-ink">Meus pedidos</h2>
    <UiAlert v-if="failure" :title="failure" />
    <UiSkeleton v-else-if="!orders" class="h-16 w-full" />
    <p v-else-if="orders.length === 0" class="text-muted">Você ainda não fez pedidos.</p>
    <ul v-else class="divide-y divide-line">
      <li v-for="order in orders" :key="order.id" class="flex flex-wrap items-center justify-between gap-3 py-3">
        <NuxtLink :to="`/pedido/${order.number}`" class="font-semibold text-primary underline underline-offset-4">Pedido {{ order.number }}</NuxtLink>
        <UiBadge :tone="orderStatusTone(order.status)">{{ orderStatusLabel(order.status) }}</UiBadge>
        <span class="text-ink">{{ money(order.total) }}</span>
      </li>
    </ul>
  </section>
</template>
