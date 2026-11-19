<script setup lang="ts">
import type { ShippingMethod } from '@whitelabel/api-client'
import { errorMessage } from '@whitelabel/auth'
import { useAuth } from '@whitelabel/panel-shell'
import { centsToInput, parseMoney, UiAlert, UiBadge, UiButton, UiInput, UiSkeleton } from '@whitelabel/ui'
import { computed, onMounted, reactive, ref, useId } from 'vue'

import { money, useCommerce } from '../commerce'

const api = useCommerce()
const auth = useAuth()
const canEdit = computed(() => auth.can('shipping:manage'))
const methods = ref<ShippingMethod[]>()
const failure = ref<string>()
const editing = ref<string>()
const kindId = useId()
const form = reactive({ name: '', kind: 'flat' as 'flat' | 'pickup', price: '', minDays: '3', maxDays: '7', active: true })

async function load() {
  methods.value = await api.shippingMethods()
}

onMounted(async () => {
  try {
    await load()
  } catch (error) {
    failure.value = errorMessage(error)
  }
})

function edit(method: ShippingMethod) {
  editing.value = method.id
  Object.assign(form, {
    name: method.name, kind: method.kind, price: centsToInput(method.price), minDays: String(method.minDays),
    maxDays: String(method.maxDays), active: method.active,
  })
}

function reset() {
  editing.value = undefined
  Object.assign(form, { name: '', kind: 'flat', price: '', minDays: '3', maxDays: '7', active: true })
}

async function save() {
  failure.value = undefined
  const price = form.kind === 'pickup' ? 0 : parseMoney(form.price)
  if (price === null) {
    failure.value = 'Informe um preço válido.'
    return
  }
  try {
    await api.saveShippingMethod(editing.value, {
      name: form.name, kind: form.kind, price, minDays: Number(form.minDays), maxDays: Number(form.maxDays), active: form.active,
    })
    reset()
    await load()
  } catch (error) {
    failure.value = errorMessage(error)
  }
}
</script>

<template>
  <div class="space-y-6">
    <h1 class="font-display text-3xl text-ink sm:text-4xl">Formas de entrega</h1>
    <p class="max-w-prose text-muted">Valores fixos configurados pela loja. A cotação com transportadoras chega em uma próxima fase.</p>
    <UiAlert v-if="failure" :title="failure" />

    <form v-if="canEdit" class="grid gap-4 rounded-card border border-line bg-surface p-5 sm:grid-cols-3 sm:p-6" @submit.prevent="save">
      <UiInput v-model="form.name" label="Nome" />
      <div class="space-y-1.5">
        <label :for="kindId" class="block text-sm font-semibold text-ink">Tipo</label>
        <select :id="kindId" v-model="form.kind" class="block min-h-11 w-full rounded-control border border-muted bg-surface px-3 text-ink">
          <option value="flat">Entrega com valor fixo</option>
          <option value="pickup">Retirada na loja</option>
        </select>
      </div>
      <UiInput v-if="form.kind === 'flat'" v-model="form.price" label="Preço (R$)" inputmode="numeric" />
      <UiInput v-model="form.minDays" label="Prazo mínimo (dias)" inputmode="numeric" />
      <UiInput v-model="form.maxDays" label="Prazo máximo (dias)" inputmode="numeric" />
      <label class="flex min-h-11 items-center gap-3 text-ink">
        <input v-model="form.active" type="checkbox" class="size-5 accent-[var(--wl-color-primary)]" />
        Disponível no checkout
      </label>
      <div class="flex gap-3 sm:col-span-3">
        <UiButton type="submit">{{ editing ? 'Salvar alterações' : 'Adicionar forma de entrega' }}</UiButton>
        <UiButton v-if="editing" @click="reset">Cancelar edição</UiButton>
      </div>
    </form>

    <UiSkeleton v-if="!methods" class="h-24 w-full" />
    <p v-else-if="methods.length === 0" class="rounded-card border border-line bg-surface p-6 text-muted">Nenhuma forma de entrega configurada.</p>
    <ul v-else class="divide-y divide-line rounded-card border border-line bg-surface px-5">
      <li v-for="method in methods" :key="method.id" class="flex flex-wrap items-center justify-between gap-3 py-4">
        <div>
          <p class="font-semibold text-ink">{{ method.name }}</p>
          <p class="text-sm text-muted">{{ method.price ? money(method.price) : 'Grátis' }} · {{ method.minDays }} a {{ method.maxDays }} dias</p>
        </div>
        <div class="flex items-center gap-3">
          <UiBadge :tone="method.active ? 'success' : 'warning'">{{ method.active ? 'Ativa' : 'Inativa' }}</UiBadge>
          <UiButton v-if="canEdit" @click="edit(method)">Editar<span class="sr-only"> {{ method.name }}</span></UiButton>
        </div>
      </li>
    </ul>
  </div>
</template>
