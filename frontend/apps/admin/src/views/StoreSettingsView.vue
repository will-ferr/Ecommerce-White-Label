<script setup lang="ts">
import type { StoreProfile } from '@whitelabel/api-client'
import { errorMessage } from '@whitelabel/auth'
import { UiAlert, UiButton, UiInput, UiSkeleton } from '@whitelabel/ui'
import { onMounted, reactive, ref } from 'vue'

import { fieldMessages, useManagement } from '../commerce'

const api = useManagement()
const profile = ref<StoreProfile>()
const form = reactive({ storeName: '', seoTitle: '', seoDescription: '', seoIndexable: false })
const failure = ref<string>()
const saved = ref(false)
const errors = ref<Record<string, string>>({})

const LABELS = {
  storeName: 'Use de 1 a 120 caracteres.',
  seoTitle: 'Use até 70 caracteres.',
  seoDescription: 'Use até 160 caracteres.',
}

onMounted(async () => {
  try {
    profile.value = await api.profile()
    Object.assign(form, profile.value)
  } catch (error) {
    failure.value = errorMessage(error)
  }
})

async function save() {
  failure.value = undefined
  saved.value = false
  errors.value = {}
  try {
    await api.saveProfile({ ...form })
    saved.value = true
  } catch (error) {
    errors.value = fieldMessages(error, LABELS)
    failure.value = Object.keys(errors.value).length ? 'Confira os campos destacados.' : errorMessage(error)
  }
}
</script>

<template>
  <div class="space-y-6">
    <h1 class="font-display text-3xl text-ink sm:text-4xl">Dados da loja</h1>
    <UiAlert v-if="failure" :title="failure" />
    <p v-if="saved" role="status" class="rounded-control bg-success-soft px-4 py-3 text-sm font-semibold text-success">Alterações salvas.</p>
    <UiSkeleton v-if="!profile && !failure" class="h-64 w-full" />
    <form v-else-if="profile" class="max-w-2xl space-y-5 rounded-card border border-line bg-surface p-5 sm:p-6" @submit.prevent="save">
      <UiInput v-model="form.storeName" label="Nome da loja" :error="errors.storeName" />
      <fieldset class="space-y-4">
        <legend class="text-lg font-semibold text-ink">Mecanismos de busca</legend>
        <UiInput v-model="form.seoTitle" label="Título nas buscas" :required="false" :error="errors.seoTitle" hint="Até 70 caracteres. Em branco, usa o nome da loja." />
        <UiInput v-model="form.seoDescription" label="Descrição nas buscas" :required="false" :error="errors.seoDescription" hint="Até 160 caracteres." />
        <label class="flex min-h-11 items-center gap-3 text-ink">
          <input v-model="form.seoIndexable" type="checkbox" class="size-5 accent-[var(--wl-color-primary)]" />
          Permitir que buscadores indexem a loja
        </label>
        <p class="text-sm text-muted">Com a indexação ligada, a loja publica o sitemap e libera os buscadores no robots.txt.</p>
      </fieldset>
      <dl class="grid gap-3 border-t border-line pt-4 text-sm sm:grid-cols-3">
        <div><dt class="text-muted">Moeda</dt><dd class="font-semibold text-ink">{{ profile.currency }}</dd></div>
        <div><dt class="text-muted">Idioma</dt><dd class="font-semibold text-ink">{{ profile.locale }}</dd></div>
        <div><dt class="text-muted">Fuso horário</dt><dd class="font-semibold text-ink">{{ profile.timezone }}</dd></div>
      </dl>
      <p class="text-sm text-muted">Moeda, idioma, fuso, plano e domínios são definidos pela plataforma.</p>
      <UiButton type="submit">Salvar</UiButton>
    </form>
  </div>
</template>
