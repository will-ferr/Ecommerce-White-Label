<script setup lang="ts">
import type { Coupon } from '@whitelabel/api-client'
import { errorMessage } from '@whitelabel/auth'
import { centsToInput, parseMoney, UiAlert, UiBadge, UiButton, UiInput, UiSkeleton } from '@whitelabel/ui'
import { onMounted, reactive, ref, useId } from 'vue'

import { fieldMessages, money, useManagement } from '../commerce'

type Kind = Coupon['kind']

const api = useManagement()
const coupons = ref<Coupon[]>()
const failure = ref<string>()
const errors = ref<Record<string, string>>({})
const editing = ref<string>()
const kindId = useId()
const empty = () => ({
  code: '', description: '', kind: 'percent' as Kind, value: '10', minSubtotal: '', startsAt: '', endsAt: '',
  usageLimit: '', perCustomerLimit: '', active: true,
})
const form = reactive(empty())

const KIND_LABELS: Record<Kind, string> = { percent: 'Porcentagem', fixed: 'Valor fixo', free_shipping: 'Frete grátis' }
const LABELS: Record<string, string> = {
  code: 'Use de 3 a 30 letras, números, hífens ou sublinhados. O código não pode se repetir.',
  value: 'Informe um valor válido. Porcentagens vão de 1 a 100.',
  minSubtotal: 'Informe um valor válido.',
  endsAt: 'O fim precisa ser depois do início.',
  usageLimit: 'Use um número positivo ou deixe em branco.',
  perCustomerLimit: 'Use um número positivo ou deixe em branco.',
}

async function load() {
  coupons.value = await api.coupons()
}

onMounted(async () => {
  try {
    await load()
  } catch (error) {
    failure.value = errorMessage(error)
  }
})

const toLocal = (value: string | null) => (value ? value.slice(0, 16) : '')
const fromLocal = (value: string) => (value ? new Date(value).toISOString() : null)
const optionalNumber = (value: string) => (value.trim() ? Number(value) : null)

function edit(coupon: Coupon) {
  editing.value = coupon.id
  errors.value = {}
  Object.assign(form, {
    code: coupon.code, description: coupon.description, kind: coupon.kind,
    value: coupon.kind === 'fixed' ? centsToInput(coupon.value) : String(coupon.value),
    minSubtotal: coupon.minSubtotal ? centsToInput(coupon.minSubtotal) : '',
    startsAt: toLocal(coupon.startsAt), endsAt: toLocal(coupon.endsAt),
    usageLimit: coupon.usageLimit?.toString() ?? '', perCustomerLimit: coupon.perCustomerLimit?.toString() ?? '', active: coupon.active,
  })
}

function reset() {
  editing.value = undefined
  errors.value = {}
  Object.assign(form, empty())
}

function describe(coupon: Coupon) {
  const benefit = coupon.kind === 'percent' ? `${coupon.value}% de desconto` : coupon.kind === 'fixed' ? `${money(coupon.value)} de desconto` : 'Frete grátis'
  const limit = coupon.usageLimit ? ` · ${coupon.usedCount ?? 0} de ${coupon.usageLimit} usos` : ` · ${coupon.usedCount ?? 0} usos`
  const minimum = coupon.minSubtotal ? ` · a partir de ${money(coupon.minSubtotal)}` : ''
  return benefit + minimum + limit
}

async function save() {
  failure.value = undefined
  errors.value = {}
  const value = form.kind === 'fixed' ? parseMoney(form.value) : form.kind === 'percent' ? Number(form.value) : 0
  const minSubtotal = form.minSubtotal.trim() ? parseMoney(form.minSubtotal) : 0
  if (value === null || Number.isNaN(value) || minSubtotal === null) {
    failure.value = 'Informe valores válidos.'
    return
  }
  try {
    await api.saveCoupon(editing.value, {
      code: form.code, description: form.description, kind: form.kind, value, minSubtotal,
      startsAt: fromLocal(form.startsAt), endsAt: fromLocal(form.endsAt),
      usageLimit: optionalNumber(form.usageLimit), perCustomerLimit: optionalNumber(form.perCustomerLimit), active: form.active,
    })
    reset()
    await load()
  } catch (error) {
    errors.value = fieldMessages(error, LABELS)
    failure.value = Object.keys(errors.value).length ? 'Confira os campos destacados.' : errorMessage(error)
  }
}
</script>

<template>
  <div class="space-y-6">
    <h1 class="font-display text-3xl text-ink sm:text-4xl">Cupons</h1>
    <UiAlert v-if="failure" :title="failure" />

    <form class="grid gap-4 rounded-card border border-line bg-surface p-5 sm:grid-cols-2 sm:p-6 lg:grid-cols-3" @submit.prevent="save">
      <div v-if="editing" class="space-y-1.5">
        <p class="text-sm font-semibold text-ink">Código</p>
        <p class="flex min-h-11 items-center font-mono text-ink">{{ form.code }}</p>
      </div>
      <UiInput v-else v-model="form.code" label="Código" :error="errors.code" hint="O comprador digita este código." />
      <UiInput v-model="form.description" label="Descrição" :required="false" :error="errors.description" />
      <div class="space-y-1.5">
        <label :for="kindId" class="block text-sm font-semibold text-ink">Tipo</label>
        <select :id="kindId" v-model="form.kind" class="block min-h-11 w-full rounded-control border border-muted bg-surface px-3 text-ink">
          <option v-for="(label, value) in KIND_LABELS" :key="value" :value="value">{{ label }}</option>
        </select>
      </div>
      <UiInput v-if="form.kind !== 'free_shipping'" v-model="form.value" :label="form.kind === 'percent' ? 'Desconto (%)' : 'Desconto (R$)'" inputmode="numeric" :error="errors.value" />
      <UiInput v-model="form.minSubtotal" label="Pedido mínimo (R$)" inputmode="numeric" :required="false" :error="errors.minSubtotal" />
      <UiInput v-model="form.startsAt" label="Início" type="datetime-local" :required="false" />
      <UiInput v-model="form.endsAt" label="Fim" type="datetime-local" :required="false" :error="errors.endsAt" />
      <UiInput v-model="form.usageLimit" label="Limite de usos" inputmode="numeric" :required="false" :error="errors.usageLimit" />
      <UiInput v-model="form.perCustomerLimit" label="Usos por cliente" inputmode="numeric" :required="false" :error="errors.perCustomerLimit" />
      <label class="flex min-h-11 items-center gap-3 text-ink">
        <input v-model="form.active" type="checkbox" class="size-5 accent-[var(--wl-color-primary)]" />
        Cupom ativo
      </label>
      <div class="flex gap-3 sm:col-span-2 lg:col-span-3">
        <UiButton type="submit">{{ editing ? 'Salvar alterações' : 'Criar cupom' }}</UiButton>
        <UiButton v-if="editing" @click="reset">Cancelar edição</UiButton>
      </div>
    </form>

    <UiSkeleton v-if="!coupons && !failure" class="h-24 w-full" />
    <p v-else-if="coupons && coupons.length === 0" class="rounded-card border border-line bg-surface p-6 text-muted">Nenhum cupom criado.</p>
    <ul v-else-if="coupons" class="divide-y divide-line rounded-card border border-line bg-surface px-5">
      <li v-for="coupon in coupons" :key="coupon.id" class="flex flex-wrap items-center justify-between gap-3 py-4">
        <div class="min-w-0">
          <p class="font-mono font-semibold text-ink">{{ coupon.code }}</p>
          <p class="text-sm text-muted">{{ describe(coupon) }}</p>
        </div>
        <div class="flex items-center gap-3">
          <UiBadge :tone="coupon.active ? 'success' : 'warning'">{{ coupon.active ? 'Ativo' : 'Pausado' }}</UiBadge>
          <UiButton @click="edit(coupon)">Editar<span class="sr-only"> o cupom {{ coupon.code }}</span></UiButton>
        </div>
      </li>
    </ul>
  </div>
</template>
