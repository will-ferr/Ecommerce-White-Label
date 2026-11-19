<script setup lang="ts">
import type { AdminProduct } from '@whitelabel/api-client'
import { errorMessage } from '@whitelabel/auth'
import { useAuth } from '@whitelabel/panel-shell'
import { UiAlert, UiBadge, UiButton, UiSkeleton } from '@whitelabel/ui'
import { onMounted, ref, useId } from 'vue'
import { RouterLink } from 'vue-router'

import { money, PRODUCT_STATUS_LABELS, useCommerce } from '../commerce'

const api = useCommerce()
const auth = useAuth()
const products = ref<AdminProduct[]>()
const cursor = ref<string>()
const search = ref('')
const status = ref('')
const failure = ref<string>()
const searchId = useId()
const statusId = useId()

async function load(append = false) {
  failure.value = undefined
  try {
    const page = await api.products({ q: search.value, status: status.value, cursor: append ? cursor.value : undefined })
    products.value = append ? [...(products.value ?? []), ...page.items] : page.items
    cursor.value = page.nextCursor
  } catch (error) {
    failure.value = errorMessage(error)
  }
}

onMounted(() => load())

const priceRange = (product: AdminProduct) => {
  const prices = product.variants.filter((variant) => variant.status !== 'archived').map((variant) => variant.price)
  if (!prices.length) return '—'
  const low = Math.min(...prices)
  const high = Math.max(...prices)
  return low === high ? money(low) : `${money(low)} – ${money(high)}`
}
const stockOf = (product: AdminProduct) =>
  product.variants.filter((variant) => variant.status !== 'archived').reduce((total, variant) => total + (variant.stock?.available ?? 0), 0)
</script>

<template>
  <div class="space-y-6">
    <div class="flex flex-wrap items-center justify-between gap-4">
      <h1 class="font-display text-3xl text-ink sm:text-4xl">Produtos</h1>
      <RouterLink v-if="auth.can('catalog:manage')" to="/produtos/novo"><UiButton>Novo produto</UiButton></RouterLink>
    </div>

    <form class="flex flex-wrap items-end gap-3" role="search" @submit.prevent="load()">
      <div class="space-y-1.5">
        <label :for="searchId" class="block text-sm font-semibold text-ink">Buscar</label>
        <input :id="searchId" v-model="search" type="search" class="block min-h-11 rounded-control border border-muted bg-surface px-3 text-ink" />
      </div>
      <div class="space-y-1.5">
        <label :for="statusId" class="block text-sm font-semibold text-ink">Situação</label>
        <select :id="statusId" v-model="status" class="block min-h-11 rounded-control border border-muted bg-surface px-3 text-ink" @change="load()">
          <option value="">Todas</option>
          <option v-for="(label, value) in PRODUCT_STATUS_LABELS" :key="value" :value="value">{{ label }}</option>
        </select>
      </div>
      <UiButton type="submit">Filtrar</UiButton>
    </form>

    <UiAlert v-if="failure" :title="failure" />
    <UiSkeleton v-else-if="!products" class="h-40 w-full" />
    <p v-else-if="products.length === 0" class="rounded-card border border-line bg-surface p-6 text-muted">Nenhum produto encontrado.</p>
    <ul v-else class="divide-y divide-line rounded-card border border-line bg-surface px-5">
      <li v-for="product in products" :key="product.id" class="flex flex-wrap items-center justify-between gap-3 py-4">
        <div class="min-w-0">
          <RouterLink :to="`/produtos/${product.id}`" class="font-semibold text-primary underline underline-offset-4">{{ product.name }}</RouterLink>
          <p class="text-sm text-muted">{{ product.variants.filter((v) => v.status !== 'archived').length }} variações · {{ priceRange(product) }}</p>
        </div>
        <div class="flex items-center gap-3">
          <span class="text-sm text-ink">{{ stockOf(product) }} disponíveis</span>
          <UiBadge :tone="product.status === 'active' ? 'success' : product.status === 'draft' ? 'warning' : 'danger'">
            {{ PRODUCT_STATUS_LABELS[product.status] }}
          </UiBadge>
        </div>
      </li>
    </ul>
    <UiButton v-if="cursor" @click="load(true)">Carregar mais</UiButton>
  </div>
</template>
