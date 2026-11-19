<script setup lang="ts">
import type { StoreSummary } from '@whitelabel/api-client'
import { ApiError } from '@whitelabel/api-client'
import { errorMessage } from '@whitelabel/auth'
import { useAuth } from '@whitelabel/panel-shell'
import { UiAlert, UiBadge, UiButton, UiInput, UiSkeleton } from '@whitelabel/ui'
import { computed, onMounted, reactive, ref, useId } from 'vue'
import { RouterLink, useRouter } from 'vue-router'

import { formatDate, STATUS_LABELS, useConsole } from '../console'

const api = useConsole()
const auth = useAuth()
const router = useRouter()
const stores = ref<StoreSummary[]>()
const baseDomain = ref('')
const plans = ref<{ code: string; name: string }[]>([])
const failure = ref<string>()
const errors = ref<Record<string, string>>({})
const filter = ref('')
const planId = useId()
const filterId = useId()
const form = reactive({ name: '', slug: '', plan: 'starter' })
const canManage = computed(() => auth.can('stores:manage'))

const visible = computed(() => {
  const text = filter.value.trim().toLowerCase()
  return (stores.value ?? []).filter((store) => !text || store.name.toLowerCase().includes(text) || store.slug.includes(text))
})

onMounted(async () => {
  try {
    const [list, available] = await Promise.all([api.stores(), api.plans()])
    stores.value = list.items
    baseDomain.value = list.baseDomain
    plans.value = available
  } catch (error) {
    failure.value = errorMessage(error)
  }
})

async function create() {
  failure.value = undefined
  errors.value = {}
  try {
    const created = await api.createStore({ ...form })
    await router.push(`/lojas/${created.slug}`)
  } catch (error) {
    if (error instanceof ApiError && error.code === 'TENANT_ALREADY_EXISTS') {
      errors.value = { slug: 'Este identificador já está em uso.' }
    } else if (error instanceof ApiError && error.fieldErrors.length) {
      errors.value = Object.fromEntries(error.fieldErrors.map((field) => [
        field.field,
        field.field === 'slug'
          ? (field.code === 'reserved' ? 'Este nome é reservado pela plataforma.' : 'Use de 3 a 63 letras minúsculas, números e hífens.')
          : 'Use de 1 a 120 caracteres.',
      ]))
    }
    failure.value = Object.keys(errors.value).length ? 'Confira os campos destacados.' : errorMessage(error)
  }
}
</script>

<template>
  <div class="space-y-6">
    <h1 class="font-display text-3xl text-ink sm:text-4xl">Lojas</h1>
    <UiAlert v-if="failure" :title="failure" />

    <form v-if="canManage" class="grid gap-4 rounded-card border border-line bg-surface p-5 sm:grid-cols-3 sm:p-6" @submit.prevent="create">
      <h2 class="text-lg font-semibold text-ink sm:col-span-3">Nova loja</h2>
      <UiInput v-model="form.name" label="Nome da loja" :error="errors.storeName" />
      <UiInput v-model="form.slug" label="Identificador" :error="errors.slug" :hint="`Endereço: ${form.slug || 'identificador'}.${baseDomain}`" />
      <div class="space-y-1.5">
        <label :for="planId" class="block text-sm font-semibold text-ink">Plano</label>
        <select :id="planId" v-model="form.plan" class="block min-h-11 w-full rounded-control border border-muted bg-surface px-3 text-ink">
          <option v-for="plan in plans" :key="plan.code" :value="plan.code">{{ plan.name }}</option>
        </select>
      </div>
      <div class="sm:col-span-3"><UiButton type="submit">Criar loja</UiButton></div>
    </form>

    <div class="space-y-1.5">
      <label :for="filterId" class="block text-sm font-semibold text-ink">Filtrar por nome</label>
      <input :id="filterId" v-model="filter" type="search" class="block min-h-11 w-full max-w-sm rounded-control border border-muted bg-surface px-3 text-ink" />
    </div>
    <UiSkeleton v-if="!stores && !failure" class="h-40 w-full" />
    <p v-else-if="stores && visible.length === 0" class="rounded-card border border-line bg-surface p-6 text-muted">Nenhuma loja encontrada.</p>
    <ul v-else-if="stores" class="divide-y divide-line rounded-card border border-line bg-surface px-5">
      <li v-for="store in visible" :key="store.slug" class="flex flex-wrap items-center justify-between gap-3 py-4">
        <div class="min-w-0">
          <RouterLink :to="`/lojas/${store.slug}`" class="font-semibold text-primary underline underline-offset-4">{{ store.name }}</RouterLink>
          <p class="text-sm break-all text-muted">{{ store.slug }}.{{ baseDomain }} · plano {{ store.plan }} · criada em {{ formatDate(store.createdAt) }}</p>
        </div>
        <UiBadge :tone="store.status === 'active' ? 'success' : 'danger'">{{ STATUS_LABELS[store.status] }}</UiBadge>
      </li>
    </ul>
  </div>
</template>
