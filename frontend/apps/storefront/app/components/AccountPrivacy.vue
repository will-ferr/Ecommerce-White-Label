<script setup lang="ts">
import { ApiError } from '@whitelabel/api-client'
import { errorMessage } from '@whitelabel/auth'
import { UiAlert, UiButton, UiInput } from '@whitelabel/ui'

const emit = defineEmits<{ erased: [] }>()

const api = useStoreApi()
const failure = ref<string>()
const exporting = ref(false)
const confirming = ref(false)
const password = ref('')
const erasing = ref(false)

async function download() {
  exporting.value = true
  failure.value = undefined
  try {
    const data = await api.exportData()
    const url = URL.createObjectURL(new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' }))
    const link = document.createElement('a')
    link.href = url
    link.download = 'meus-dados.json'
    link.click()
    URL.revokeObjectURL(url)
  } catch (error) {
    failure.value = errorMessage(error)
  } finally {
    exporting.value = false
  }
}

async function erase() {
  erasing.value = true
  failure.value = undefined
  try {
    await api.eraseAccount(password.value)
    emit('erased')
  } catch (error) {
    failure.value = error instanceof ApiError && error.code === 'INVALID_PASSWORD' ? 'Senha incorreta.' : errorMessage(error)
  } finally {
    erasing.value = false
  }
}
</script>

<template>
  <section class="space-y-4 rounded-card border border-line bg-surface p-5 sm:p-6" aria-labelledby="privacy-title">
    <h2 id="privacy-title" class="font-display text-2xl text-ink">Privacidade e dados</h2>
    <UiAlert v-if="failure" :title="failure" />
    <div class="space-y-2">
      <p class="text-muted">Baixe uma cópia dos dados da sua conta: cadastro, endereços, pedidos e histórico de acessos.</p>
      <UiButton :disabled="exporting" @click="download">{{ exporting ? 'Preparando…' : 'Baixar meus dados' }}</UiButton>
    </div>
    <div class="space-y-2 border-t border-line pt-4">
      <p class="text-muted">
        Excluir a conta apaga seu cadastro, endereços e carrinho. Os pedidos já feitos são mantidos com o nome e o e-mail
        usados na compra, pelo prazo exigido pela legislação fiscal.
      </p>
      <UiButton v-if="!confirming" @click="confirming = true">Excluir minha conta</UiButton>
      <form v-else class="space-y-3" @submit.prevent="erase">
        <UiInput v-model="password" label="Senha para confirmar a exclusão" type="password" autocomplete="current-password" />
        <div class="flex flex-wrap gap-3">
          <UiButton type="submit" :disabled="erasing || !password">{{ erasing ? 'Excluindo…' : 'Excluir definitivamente' }}</UiButton>
          <UiButton @click="confirming = false; password = ''">Cancelar</UiButton>
        </div>
      </form>
    </div>
  </section>
</template>
