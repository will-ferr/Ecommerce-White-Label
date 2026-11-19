<script setup lang="ts">
import type { SalesReport } from '@whitelabel/api-client'
import { errorMessage } from '@whitelabel/auth'
import { UiAlert, UiButton, UiInput, UiSkeleton } from '@whitelabel/ui'
import { computed, onMounted, ref } from 'vue'

import { fieldMessages, formatDay, money, useManagement } from '../commerce'

const api = useManagement()
const report = ref<SalesReport>()
const from = ref('')
const to = ref('')
const failure = ref<string>()
const errors = ref<Record<string, string>>({})
const hovered = ref<number>()

async function load() {
  failure.value = undefined
  errors.value = {}
  try {
    report.value = await api.salesReport(from.value || undefined, to.value || undefined)
    from.value = report.value.from
    to.value = report.value.to
  } catch (error) {
    errors.value = fieldMessages(error, { from: 'Escolha um período válido de até 366 dias.', to: 'Escolha uma data válida.' })
    failure.value = Object.keys(errors.value).length ? 'Confira o período.' : errorMessage(error)
  }
}

onMounted(load)

const tiles = computed(() => {
  if (!report.value) return []
  const r = report.value
  return [
    { label: 'Receita', value: money(r.revenue) },
    { label: 'Pedidos pagos', value: String(r.paidOrders) },
    { label: 'Ticket médio', value: money(r.averageTicket) },
    { label: 'Descontos concedidos', value: money(r.discounts) },
    { label: 'Pedidos cancelados', value: String(r.canceledOrders) },
  ]
})

const peak = computed(() => Math.max(1, ...(report.value?.days.map((day) => day.revenue) ?? [1])))
const hoveredDay = computed(() => (hovered.value === undefined ? undefined : report.value?.days[hovered.value]))
</script>

<template>
  <div class="space-y-6">
    <h1 class="font-display text-3xl text-ink sm:text-4xl">Relatórios</h1>

    <form class="flex flex-wrap items-end gap-3" @submit.prevent="load">
      <UiInput v-model="from" label="De" type="date" :error="errors.from" />
      <UiInput v-model="to" label="Até" type="date" :error="errors.to" />
      <UiButton type="submit">Atualizar</UiButton>
    </form>
    <UiAlert v-if="failure" :title="failure" />
    <UiSkeleton v-if="!report && !failure" class="h-64 w-full" />

    <template v-if="report">
      <p class="text-sm text-muted">
        Vendas pagas de {{ formatDay(report.from) }} a {{ formatDay(report.to) }}, no horário da loja ({{ report.timezone }}).
        Pedidos aguardando pagamento ou cancelados não entram na receita.
      </p>
      <dl class="grid grid-cols-2 gap-4 lg:grid-cols-5">
        <div v-for="tile in tiles" :key="tile.label" class="rounded-card border border-line bg-surface p-4">
          <dt class="text-sm text-muted">{{ tile.label }}</dt>
          <dd class="mt-1 text-2xl font-semibold text-ink" :data-testid="`tile-${tile.label}`">{{ tile.value }}</dd>
        </div>
      </dl>

      <section aria-labelledby="daily-title" class="space-y-3 rounded-card border border-line bg-surface p-5">
        <div class="flex flex-wrap items-baseline justify-between gap-2">
          <h2 id="daily-title" class="text-lg font-semibold text-ink">Receita por dia</h2>
          <p class="min-h-6 text-sm text-ink" aria-live="polite">
            <template v-if="hoveredDay">{{ formatDay(hoveredDay.date) }}: <strong>{{ money(hoveredDay.revenue) }}</strong> em {{ hoveredDay.orders }} {{ hoveredDay.orders === 1 ? 'pedido' : 'pedidos' }}</template>
          </p>
        </div>
        <div class="relative h-48 border-b border-line" aria-hidden="true" @mouseleave="hovered = undefined">
          <div class="flex h-full items-end gap-0.5">
            <div
              v-for="(day, index) in report.days" :key="day.date" class="flex h-full flex-1 items-end"
              @mouseenter="hovered = index"
            >
              <div
                class="w-full rounded-t-[4px] bg-primary transition-opacity"
                :class="hovered !== undefined && hovered !== index ? 'opacity-50' : ''"
                :style="{ height: day.revenue ? `${Math.max(2, (day.revenue / peak) * 100)}%` : '0' }"
              ></div>
            </div>
          </div>
        </div>
        <div class="flex justify-between text-xs text-muted" aria-hidden="true">
          <span>{{ formatDay(report.from) }}</span><span>{{ formatDay(report.to) }}</span>
        </div>
        <details>
          <summary class="min-h-11 cursor-pointer py-2 text-sm font-semibold text-primary">Ver os valores em tabela</summary>
          <div class="max-h-72 overflow-auto">
            <table class="w-full text-sm">
              <caption class="sr-only">Receita e pedidos pagos por dia</caption>
              <thead><tr class="text-left text-muted"><th scope="col" class="py-1">Dia</th><th scope="col">Pedidos</th><th scope="col" class="text-right">Receita</th></tr></thead>
              <tbody>
                <tr v-for="day in report.days" :key="day.date" class="border-t border-line text-ink">
                  <th scope="row" class="py-1 text-left font-normal">{{ formatDay(day.date) }}</th><td>{{ day.orders }}</td><td class="text-right">{{ money(day.revenue) }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </details>
      </section>

      <section aria-labelledby="top-title" class="space-y-3 rounded-card border border-line bg-surface p-5">
        <h2 id="top-title" class="text-lg font-semibold text-ink">Produtos mais vendidos</h2>
        <p v-if="report.topProducts.length === 0" class="text-muted">Nenhuma venda paga no período.</p>
        <table v-else class="w-full text-sm">
          <thead><tr class="text-left text-muted"><th scope="col" class="py-1">Produto</th><th scope="col">Unidades</th><th scope="col" class="text-right">Receita</th></tr></thead>
          <tbody>
            <tr v-for="product in report.topProducts" :key="product.name" class="border-t border-line text-ink">
              <th scope="row" class="py-2 text-left font-normal">{{ product.name }}</th><td>{{ product.quantity }}</td><td class="text-right">{{ money(product.revenue) }}</td>
            </tr>
          </tbody>
        </table>
      </section>
    </template>
  </div>
</template>
