<script setup lang="ts">
import type { ContentPage } from '@whitelabel/api-client'
import { errorMessage } from '@whitelabel/auth'
import { RichText, UiAlert, UiBadge, UiButton, UiInput, UiSkeleton } from '@whitelabel/ui'
import { onMounted, reactive, ref, useId } from 'vue'

import { fieldMessages, formatDate, useManagement } from '../commerce'

const api = useManagement()
const pages = ref<ContentPage[]>()
const failure = ref<string>()
const errors = ref<Record<string, string>>({})
const editing = ref<ContentPage>()
const open = ref(false)
const bodyId = useId()
const helpId = useId()
const empty = () => ({ title: '', slug: '', body: '', seoDescription: '', status: 'draft' as ContentPage['status'], showInFooter: true, version: 0 })
const form = reactive(empty())

const LABELS: Record<string, string> = {
  title: 'Informe um título de até 120 caracteres.',
  slug: 'Use letras minúsculas, números e hífens. O endereço não pode se repetir.',
  body: 'Use até 20.000 caracteres.',
  seoDescription: 'Use até 160 caracteres.',
}

async function load() {
  pages.value = await api.pages()
}

onMounted(async () => {
  try {
    await load()
  } catch (error) {
    failure.value = errorMessage(error)
  }
})

function start(page?: ContentPage) {
  editing.value = page
  errors.value = {}
  failure.value = undefined
  Object.assign(form, page ? { ...page } : empty())
  open.value = true
}

async function save() {
  failure.value = undefined
  errors.value = {}
  try {
    await api.savePage(editing.value?.id, { ...form })
    open.value = false
    await load()
  } catch (error) {
    errors.value = fieldMessages(error, LABELS)
    failure.value = Object.keys(errors.value).length ? 'Confira os campos destacados.' : errorMessage(error)
  }
}

async function remove(page: ContentPage) {
  if (!window.confirm(`Excluir a página "${page.title}"?`)) return
  try {
    await api.deletePage(page.id)
    await load()
  } catch (error) {
    failure.value = errorMessage(error)
  }
}
</script>

<template>
  <div class="space-y-6">
    <div class="flex flex-wrap items-center justify-between gap-4">
      <h1 class="font-display text-3xl text-ink sm:text-4xl">Páginas</h1>
      <UiButton v-if="!open" @click="start()">Nova página</UiButton>
    </div>
    <p class="max-w-prose text-muted">Páginas institucionais, como "Sobre nós", trocas e política de privacidade. As publicadas aparecem no rodapé da loja.</p>
    <UiAlert v-if="failure" :title="failure" />

    <form v-if="open" class="grid gap-6 rounded-card border border-line bg-surface p-5 sm:p-6 lg:grid-cols-2" @submit.prevent="save">
      <div class="space-y-4">
        <UiInput v-model="form.title" label="Título" :error="errors.title" />
        <UiInput v-model="form.slug" label="Endereço" :required="false" :error="errors.slug" hint="Em branco, é criado a partir do título." />
        <div class="space-y-1.5">
          <label :for="bodyId" class="block text-sm font-semibold text-ink">Conteúdo</label>
          <p :id="helpId" class="text-sm text-muted">
            Linha em branco separa parágrafos. Use "## " para títulos, "- " para listas, **negrito** e [texto](https://endereço).
          </p>
          <textarea
            :id="bodyId" v-model="form.body" rows="14" :aria-describedby="helpId" :aria-invalid="errors.body ? 'true' : undefined"
            class="block w-full rounded-control border border-muted bg-surface p-3 text-ink"
          ></textarea>
          <p v-if="errors.body" class="text-sm text-danger">{{ errors.body }}</p>
        </div>
        <UiInput v-model="form.seoDescription" label="Descrição nas buscas" :required="false" :error="errors.seoDescription" />
        <fieldset class="space-y-2">
          <legend class="text-sm font-semibold text-ink">Situação</legend>
          <label class="flex min-h-11 items-center gap-3 text-ink"><input v-model="form.status" type="radio" value="draft" class="size-5 accent-[var(--wl-color-primary)]" /> Rascunho</label>
          <label class="flex min-h-11 items-center gap-3 text-ink"><input v-model="form.status" type="radio" value="published" class="size-5 accent-[var(--wl-color-primary)]" /> Publicada</label>
        </fieldset>
        <label class="flex min-h-11 items-center gap-3 text-ink">
          <input v-model="form.showInFooter" type="checkbox" class="size-5 accent-[var(--wl-color-primary)]" />
          Mostrar no rodapé da loja
        </label>
        <div class="flex gap-3">
          <UiButton type="submit">Salvar página</UiButton>
          <UiButton @click="open = false">Cancelar</UiButton>
        </div>
      </div>
      <section aria-labelledby="page-preview" class="space-y-3 rounded-card border border-line p-5">
        <h2 id="page-preview" class="text-sm font-semibold uppercase tracking-wide text-muted">Prévia</h2>
        <p class="font-display text-3xl text-ink">{{ form.title || 'Título da página' }}</p>
        <RichText :source="form.body" />
      </section>
    </form>

    <UiSkeleton v-if="!pages && !failure" class="h-24 w-full" />
    <p v-else-if="pages && pages.length === 0" class="rounded-card border border-line bg-surface p-6 text-muted">Nenhuma página criada.</p>
    <ul v-else-if="pages" class="divide-y divide-line rounded-card border border-line bg-surface px-5">
      <li v-for="page in pages" :key="page.id" class="flex flex-wrap items-center justify-between gap-3 py-4">
        <div class="min-w-0">
          <p class="font-semibold text-ink">{{ page.title }}</p>
          <p class="text-sm break-all text-muted">/paginas/{{ page.slug }} · alterada em {{ formatDate(page.updatedAt) }}</p>
        </div>
        <div class="flex flex-wrap items-center gap-3">
          <UiBadge :tone="page.status === 'published' ? 'success' : 'warning'">{{ page.status === 'published' ? 'Publicada' : 'Rascunho' }}</UiBadge>
          <UiButton @click="start(page)">Editar<span class="sr-only"> {{ page.title }}</span></UiButton>
          <UiButton @click="remove(page)">Excluir<span class="sr-only"> {{ page.title }}</span></UiButton>
        </div>
      </li>
    </ul>
  </div>
</template>
