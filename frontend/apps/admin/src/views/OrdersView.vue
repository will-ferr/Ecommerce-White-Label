<script setup lang="ts">
import type { Order } from '@whitelabel/api-client'
import { errorMessage } from '@whitelabel/auth'
import { useAuth } from '@whitelabel/panel-shell'
import { ORDER_STATUS_LABELS, orderStatusLabel, orderStatusTone, UiAlert, UiBadge, UiButton, UiSkeleton } from '@whitelabel/ui'
import { onMounted, ref, useId } from 'vue'
import { RouterLink } from 'vue-router'

import { formatDate, money, useCommerce } from '../commerce'

const api = useCommerce()
const auth = useAuth()
const orders = ref<Order[]>()
const cursor = ref<string>()
const status = ref('')
const search = ref('')
const failure = ref<string>()
const statusId = useId()
const searchId = useId()

async function load(append = false) {
  failure.value = undefined
  try {
    const page = await api.orders({ status: status.value, q: search.value, cursor: append ? cursor.value : undefined })
    orders.value = append ? [...(orders.value ?? []), ...page.items] : page.items
    cursor.value = page.nextCursor
  } catch (error) {
    failure.value = errorMessage(error)
  }
}

async function exportCsv() {
  try {
    const params = new URLSearchParams()
    if (status.value) params.set('status', status.value)
    if (search.value) params.set('q', search.value)
    const blob = await auth.client().download(`/api/v1/staff/orders/export?${params}`)
    const link = document.createElement('a')
    link.href = URL.createObjectURL(blob)
    link.download = 'pedidos.csv'
    link.click()
    URL.revokeObjectURL(link.href)
  } catch (error) {
    failure.value = errorMessage(error)
  }
}

onMounted(() => load())
</script>

<template>
  <div class="space-y-6">
    <div class="flex flex-wrap items-center justify-between gap-4">
      <h1 class="font-display text-3xl text-ink sm:text-4xl">Pedidos</h1>
      <UiButton @click="exportCsv">Exportar CSV</UiButton>
    </div>

    <form class="flex flex-wrap items-end gap-3" role="search" @submit.prevent="load()">
      <div class="space-y-1.5">
        <label :for="searchId" class="block text-sm font-semibold text-ink">Número ou e-mail</label>
        <input :id="searchId" v-model="search" type="search" class="block min-h-11 rounded-control border border-muted bg-surface px-3 text-ink" />
      </div>
      <div class="space-y-1.5">
        <label :for="statusId" class="block text-sm font-semibold text-ink">Situação</label>
        <select :id="statusId" v-model="status" class="block min-h-11 rounded-control border border-muted bg-surface px-3 text-ink" @change="load()">
          <option value="">Todas</option>
          <option v-for="(label, value) in ORDER_STATUS_LABELS" :key="value" :value="value">{{ label }}</option>
        </select>
      </div>
      <UiButton type="submit">Filtrar</UiButton>
    </form>

    <UiAlert v-if="failure" :title="failure" />
    <UiSkeleton v-else-if="!orders" class="h-40 w-full" />
    <p v-else-if="orders.length === 0" class="rounded-card border border-line bg-surface p-6 text-muted">Nenhum pedido encontrado.</p>
    <ul v-else class="divide-y divide-line rounded-card border border-line bg-surface px-5">
      <li v-for="order in orders" :key="order.id" class="flex flex-wrap items-center justify-between gap-3 py-4">
        <div class="min-w-0">
          <RouterLink :to="`/pedidos/${order.id}`" class="font-semibold text-primary underline underline-offset-4">Pedido {{ order.number }}</RouterLink>
          <p class="text-sm break-all text-muted">{{ order.customerName }} · {{ formatDate(order.createdAt) }}</p>
        </div>
        <div class="flex items-center gap-3">
          <span class="font-semibold text-ink">{{ money(order.total) }}</span>
          <UiBadge :tone="orderStatusTone(order.status)">{{ orderStatusLabel(order.status) }}</UiBadge>
        </div>
      </li>
    </ul>
    <UiButton v-if="cursor" @click="load(true)">Carregar mais</UiButton>
  </div>
</template>
