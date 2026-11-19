<script setup lang="ts">
import type { CustomerSummary, Order } from '@whitelabel/api-client'
import { errorMessage } from '@whitelabel/auth'
import { useAuth } from '@whitelabel/panel-shell'
import { orderStatusLabel, orderStatusTone, UiAlert, UiBadge, UiButton, UiSkeleton } from '@whitelabel/ui'
import { onMounted, ref, useId } from 'vue'
import { RouterLink } from 'vue-router'

import { formatDate, money, useCommerce, useManagement } from '../commerce'

const api = useManagement()
const commerce = useCommerce()
const auth = useAuth()
const customers = ref<CustomerSummary[]>()
const cursor = ref<string>()
const search = ref('')
const failure = ref<string>()
const searchId = useId()
const opened = ref<string>()
const orders = ref<Order[]>()

async function load(append = false) {
  failure.value = undefined
  try {
    const page = await api.customers({ q: search.value, cursor: append ? cursor.value : undefined })
    customers.value = append ? [...(customers.value ?? []), ...page.items] : page.items
    cursor.value = page.nextCursor
  } catch (error) {
    failure.value = errorMessage(error)
  }
}

async function toggle(customer: CustomerSummary) {
  if (opened.value === customer.id) {
    opened.value = undefined
    return
  }
  opened.value = customer.id
  orders.value = undefined
  if (!auth.can('orders:read')) return
  try {
    orders.value = (await commerce.orders({ customer: customer.id })).items
  } catch (error) {
    failure.value = errorMessage(error)
  }
}

onMounted(() => load())
</script>

<template>
  <div class="space-y-6">
    <h1 class="font-display text-3xl text-ink sm:text-4xl">Clientes</h1>
    <p class="max-w-prose text-muted">Contas criadas na loja. Contas excluídas a pedido do cliente não aparecem.</p>

    <form class="flex flex-wrap items-end gap-3" role="search" @submit.prevent="load()">
      <div class="space-y-1.5">
        <label :for="searchId" class="block text-sm font-semibold text-ink">Nome ou e-mail</label>
        <input :id="searchId" v-model="search" type="search" class="block min-h-11 rounded-control border border-muted bg-surface px-3 text-ink" />
      </div>
      <UiButton type="submit">Buscar</UiButton>
    </form>

    <UiAlert v-if="failure" :title="failure" />
    <UiSkeleton v-if="!customers && !failure" class="h-40 w-full" />
    <p v-else-if="customers && customers.length === 0" class="rounded-card border border-line bg-surface p-6 text-muted">Nenhum cliente encontrado.</p>
    <ul v-else-if="customers" class="divide-y divide-line rounded-card border border-line bg-surface px-5">
      <li v-for="customer in customers" :key="customer.id" class="py-4">
        <div class="flex flex-wrap items-center justify-between gap-3">
          <div class="min-w-0">
            <p class="font-semibold text-ink">{{ customer.name }}</p>
            <p class="text-sm break-all text-muted">{{ customer.email }} · desde {{ formatDate(customer.createdAt) }}</p>
          </div>
          <div class="flex items-center gap-3">
            <UiBadge :tone="customer.emailVerified ? 'success' : 'warning'">{{ customer.emailVerified ? 'E-mail confirmado' : 'E-mail não confirmado' }}</UiBadge>
            <UiButton v-if="auth.can('orders:read')" :aria-expanded="opened === customer.id" @click="toggle(customer)">
              {{ opened === customer.id ? 'Ocultar pedidos' : 'Ver pedidos' }}<span class="sr-only"> de {{ customer.name }}</span>
            </UiButton>
          </div>
        </div>
        <div v-if="opened === customer.id" class="mt-3">
          <UiSkeleton v-if="!orders" class="h-12 w-full" />
          <p v-else-if="orders.length === 0" class="text-sm text-muted">Nenhum pedido feito com esta conta.</p>
          <ul v-else class="space-y-2">
            <li v-for="order in orders" :key="order.id" class="flex flex-wrap items-center justify-between gap-3 text-sm">
              <RouterLink :to="`/pedidos/${order.id}`" class="font-semibold text-primary underline underline-offset-4">Pedido {{ order.number }}</RouterLink>
              <span class="flex items-center gap-3"><span class="text-ink">{{ money(order.total) }}</span><UiBadge :tone="orderStatusTone(order.status)">{{ orderStatusLabel(order.status) }}</UiBadge></span>
            </li>
          </ul>
        </div>
      </li>
    </ul>
    <UiButton v-if="cursor" @click="load(true)">Carregar mais</UiButton>
  </div>
</template>
