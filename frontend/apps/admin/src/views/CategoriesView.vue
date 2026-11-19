<script setup lang="ts">
import { ApiError, type Category } from '@whitelabel/api-client'
import { errorMessage } from '@whitelabel/auth'
import { useAuth } from '@whitelabel/panel-shell'
import { UiAlert, UiButton, UiInput, UiSkeleton } from '@whitelabel/ui'
import { computed, onMounted, ref } from 'vue'

import { useCommerce } from '../commerce'

const api = useCommerce()
const auth = useAuth()
const canEdit = computed(() => auth.can('catalog:manage'))
const categories = ref<Category[]>()
const name = ref('')
const failure = ref<string>()

async function run(action: () => Promise<unknown>) {
  failure.value = undefined
  try {
    await action()
    categories.value = await api.categories()
  } catch (error) {
    failure.value = error instanceof ApiError && error.code === 'CATEGORY_IN_USE'
      ? 'Mova os produtos e subcategorias antes de excluir esta categoria.'
      : error instanceof ApiError && error.code === 'SLUG_IN_USE'
        ? 'Já existe uma categoria com este nome.'
        : errorMessage(error)
  }
}

onMounted(() => run(async () => undefined))

const create = () =>
  run(async () => {
    await api.createCategory({ name: name.value })
    name.value = ''
  })
</script>

<template>
  <div class="space-y-6">
    <h1 class="font-display text-3xl text-ink sm:text-4xl">Categorias</h1>
    <UiAlert v-if="failure" :title="failure" />

    <form v-if="canEdit" class="flex flex-wrap items-end gap-3" @submit.prevent="create">
      <UiInput v-model="name" label="Nova categoria" />
      <UiButton type="submit">Criar</UiButton>
    </form>

    <UiSkeleton v-if="!categories" class="h-24 w-full" />
    <p v-else-if="categories.length === 0" class="rounded-card border border-line bg-surface p-6 text-muted">Nenhuma categoria criada.</p>
    <ul v-else class="divide-y divide-line rounded-card border border-line bg-surface px-5">
      <li v-for="category in categories" :key="category.id" class="flex flex-wrap items-center justify-between gap-3 py-4">
        <div>
          <p class="font-semibold text-ink">{{ category.name }}</p>
          <p class="text-sm text-muted">/c/{{ category.slug }}</p>
        </div>
        <UiButton v-if="canEdit" @click="run(() => api.deleteCategory(category.id))">
          Excluir<span class="sr-only"> {{ category.name }}</span>
        </UiButton>
      </li>
    </ul>
  </div>
</template>
