<script setup lang="ts">
import type { Order } from '@whitelabel/api-client'
import { orderStatusLabel, orderStatusTone, UiBadge } from '@whitelabel/ui'

defineProps<{ order: Order }>()

const money = useMoney()
const date = (value: string) => new Intl.DateTimeFormat('pt-BR', { dateStyle: 'short', timeStyle: 'short' }).format(new Date(value))
</script>

<template>
  <div class="space-y-6">
    <div class="flex flex-wrap items-center gap-3">
      <UiBadge :tone="orderStatusTone(order.status)" data-testid="order-status">{{ orderStatusLabel(order.status) }}</UiBadge>
      <span class="text-sm text-muted">Feito em {{ date(order.createdAt) }}</span>
    </div>
    <p v-if="order.trackingCode" class="text-ink">Código de rastreio: <strong>{{ order.trackingCode }}</strong></p>
    <p v-if="order.cancelReason" class="text-ink">Motivo do cancelamento: {{ order.cancelReason }}</p>

    <section class="rounded-card border border-line bg-surface p-5" aria-labelledby="items-title">
      <h2 id="items-title" class="font-display text-2xl text-ink">Itens</h2>
      <ul class="mt-3 divide-y divide-line">
        <li v-for="item in order.items" :key="item.variantId" class="flex justify-between gap-4 py-3">
          <span class="text-ink">{{ item.quantity }}× {{ item.productName }}<span v-if="item.label" class="text-muted"> ({{ item.label }})</span></span>
          <span class="shrink-0 text-ink">{{ money(item.total) }}</span>
        </li>
      </ul>
      <dl class="mt-3 space-y-2 border-t border-line pt-3 text-ink">
        <div class="flex justify-between"><dt>Subtotal</dt><dd>{{ money(order.subtotal) }}</dd></div>
        <div class="flex justify-between"><dt>Frete ({{ order.shippingMethod }})</dt><dd>{{ money(order.shippingAmount) }}</dd></div>
        <div class="flex justify-between text-lg font-semibold"><dt>Total</dt><dd>{{ money(order.total) }}</dd></div>
      </dl>
    </section>

    <section class="rounded-card border border-line bg-surface p-5" aria-labelledby="address-title">
      <h2 id="address-title" class="font-display text-2xl text-ink">Entrega</h2>
      <p class="mt-2 text-ink">
        {{ order.shippingAddress.recipient }}<br />
        {{ order.shippingAddress.street }}, {{ order.shippingAddress.number }}<span v-if="order.shippingAddress.complement"> – {{ order.shippingAddress.complement }}</span><br />
        {{ order.shippingAddress.district }}, {{ order.shippingAddress.city }}/{{ order.shippingAddress.state }} – CEP {{ order.shippingAddress.postalCode }}
      </p>
    </section>
  </div>
</template>
