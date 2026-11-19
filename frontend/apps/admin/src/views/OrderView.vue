<script setup lang="ts">
import type { Order } from '@whitelabel/api-client'
import { errorMessage } from '@whitelabel/auth'
import { useAuth } from '@whitelabel/panel-shell'
import { orderStatusLabel, orderStatusTone, paymentStatusLabel, UiAlert, UiBadge, UiButton, UiInput, UiSkeleton } from '@whitelabel/ui'
import { computed, onMounted, ref, useId } from 'vue'

import { formatDate, money, useCommerce } from '../commerce'

const props = defineProps<{ id: string }>()

const api = useCommerce()
const auth = useAuth()
const order = ref<Order>()
const failure = ref<string>()
const notice = ref<string>()
const tracking = ref('')
const reason = ref('')
const note = ref('')
const noteId = useId()
const canManage = computed(() => auth.can('orders:manage'))

const ACTION_LABELS: Record<string, string> = {
  processing: 'Iniciar separação', shipped: 'Marcar como enviado', delivered: 'Confirmar entrega',
  delivery_failed: 'Registrar falha na entrega', completed: 'Concluir pedido', canceled: 'Cancelar pedido',
}

async function load() {
  order.value = await api.order(props.id)
}

onMounted(async () => {
  try {
    await load()
  } catch (error) {
    failure.value = errorMessage(error)
  }
})

async function run(action: () => Promise<void>, success: string) {
  failure.value = undefined
  notice.value = undefined
  try {
    await action()
    await load()
    notice.value = success
  } catch (error) {
    failure.value = errorMessage(error)
  }
}

const move = (status: string) =>
  run(async () => {
    await api.changeStatus(props.id, { status, trackingCode: status === 'shipped' ? tracking.value : undefined, note: status === 'canceled' ? reason.value : undefined })
    tracking.value = ''
    reason.value = ''
  }, 'Pedido atualizado.')

const addNote = () =>
  run(async () => {
    await api.addNote(props.id, note.value)
    note.value = ''
  }, 'Nota adicionada.')
</script>

<template>
  <div class="space-y-6">
    <UiAlert v-if="failure" :title="failure" />
    <p v-if="notice" role="status" class="rounded-control bg-success-soft px-4 py-3 text-sm font-semibold text-success">{{ notice }}</p>
    <UiSkeleton v-if="!order && !failure" class="h-64 w-full" />

    <template v-if="order">
      <div class="flex flex-wrap items-center gap-4">
        <h1 class="font-display text-3xl text-ink sm:text-4xl">Pedido {{ order.number }}</h1>
        <UiBadge :tone="orderStatusTone(order.status)" data-testid="order-status">{{ orderStatusLabel(order.status) }}</UiBadge>
      </div>

      <section v-if="canManage && order.nextStatuses?.length" class="space-y-4 rounded-card border border-line bg-surface p-5 sm:p-6" aria-labelledby="actions-title">
        <h2 id="actions-title" class="font-display text-2xl text-ink">Próximos passos</h2>
        <UiInput v-if="order.nextStatuses.includes('shipped')" v-model="tracking" label="Código de rastreio" :required="false" />
        <UiInput v-if="order.nextStatuses.includes('canceled')" v-model="reason" label="Motivo do cancelamento" :required="false" />
        <div class="flex flex-wrap gap-3">
          <UiButton v-for="status in order.nextStatuses" :key="status" @click="move(status)">{{ ACTION_LABELS[status] ?? status }}</UiButton>
        </div>
        <p v-if="order.nextStatuses.includes('canceled') && order.status !== 'pending_payment'" class="text-sm text-muted">
          Cancelar um pedido pago estorna o pagamento e devolve os itens ao estoque.
        </p>
      </section>

      <div class="grid gap-6 lg:grid-cols-2">
        <section class="rounded-card border border-line bg-surface p-5" aria-labelledby="items-title">
          <h2 id="items-title" class="font-display text-2xl text-ink">Itens</h2>
          <ul class="mt-3 divide-y divide-line">
            <li v-for="item in order.items" :key="item.variantId" class="flex justify-between gap-4 py-3">
              <span class="text-ink">{{ item.quantity }}× {{ item.productName }}<span class="ml-1 text-muted">{{ item.label }} · {{ item.sku }}</span></span>
              <span class="shrink-0 text-ink">{{ money(item.total) }}</span>
            </li>
          </ul>
          <dl class="mt-3 space-y-2 border-t border-line pt-3 text-ink">
            <div class="flex justify-between"><dt>Subtotal</dt><dd>{{ money(order.subtotal) }}</dd></div>
            <div class="flex justify-between"><dt>Frete ({{ order.shippingMethod }})</dt><dd>{{ money(order.shippingAmount) }}</dd></div>
            <div class="flex justify-between font-semibold"><dt>Total</dt><dd>{{ money(order.total) }}</dd></div>
            <div class="flex justify-between text-sm text-muted"><dt>Pagamento</dt><dd>{{ order.payment?.label }} · {{ paymentStatusLabel(order.payment?.status ?? '') }}</dd></div>
          </dl>
        </section>

        <section class="space-y-2 rounded-card border border-line bg-surface p-5" aria-labelledby="customer-title">
          <h2 id="customer-title" class="font-display text-2xl text-ink">Cliente e entrega</h2>
          <p class="break-all text-ink">{{ order.customerName }} · {{ order.customerEmail }}</p>
          <p class="text-ink">
            {{ order.shippingAddress.street }}, {{ order.shippingAddress.number }}<span v-if="order.shippingAddress.complement"> – {{ order.shippingAddress.complement }}</span><br />
            {{ order.shippingAddress.district }}, {{ order.shippingAddress.city }}/{{ order.shippingAddress.state }} – CEP {{ order.shippingAddress.postalCode }}
          </p>
          <p v-if="order.trackingCode" class="text-ink">Rastreio: <strong>{{ order.trackingCode }}</strong></p>
          <p v-if="order.cancelReason" class="text-ink">Cancelamento: {{ order.cancelReason }}</p>
        </section>
      </div>

      <section class="rounded-card border border-line bg-surface p-5" aria-labelledby="history-title">
        <h2 id="history-title" class="font-display text-2xl text-ink">Histórico</h2>
        <ol class="mt-3 space-y-2">
          <li v-for="(entry, index) in order.history" :key="index" class="text-ink">
            <span class="font-semibold">{{ orderStatusLabel(entry.to) }}</span>
            <span class="text-sm text-muted"> · {{ formatDate(entry.createdAt) }}<span v-if="entry.note"> · {{ entry.note }}</span></span>
          </li>
        </ol>
      </section>

      <section class="space-y-3 rounded-card border border-line bg-surface p-5" aria-labelledby="notes-title">
        <h2 id="notes-title" class="font-display text-2xl text-ink">Notas internas</h2>
        <ul v-if="order.notes?.length" class="space-y-2">
          <li v-for="(entry, index) in order.notes" :key="index" class="text-ink">
            {{ entry.body }} <span class="text-sm text-muted">· {{ formatDate(entry.createdAt) }}</span>
          </li>
        </ul>
        <p v-else class="text-muted">Nenhuma nota.</p>
        <form v-if="canManage" class="space-y-2" @submit.prevent="addNote">
          <label :for="noteId" class="block text-sm font-semibold text-ink">Nova nota</label>
          <textarea :id="noteId" v-model="note" rows="3" maxlength="2000" class="block w-full rounded-control border border-muted bg-surface p-3 text-ink"></textarea>
          <UiButton type="submit">Adicionar nota</UiButton>
        </form>
      </section>
    </template>
  </div>
</template>
