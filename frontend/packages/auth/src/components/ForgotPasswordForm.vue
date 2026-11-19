<script setup lang="ts">
import { UiAlert, UiButton, UiInput } from '@whitelabel/ui'
import { ref } from 'vue'

import { errorMessage } from '../messages'
import type { AuthStore } from '../store'

const props = withDefaults(defineProps<{ auth: AuthStore; askStore?: boolean }>(), { askStore: false })

const store = ref('')
const email = ref('')
const sent = ref(false)
const failure = ref<string>()
const busy = ref(false)

async function submit() {
  busy.value = true
  failure.value = undefined
  try {
    await props.auth.client().forgotPassword({
      email: email.value,
      ...(props.askStore ? { store: store.value.trim().toLowerCase() } : {}),
    })
    sent.value = true
  } catch (error) {
    failure.value = errorMessage(error)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <p v-if="sent" role="status" class="text-ink">
    Se houver uma conta com este e-mail, enviaremos as instruções em alguns instantes.
  </p>
  <form v-else class="space-y-4" @submit.prevent="submit">
    <UiAlert v-if="failure" :title="failure" />
    <UiInput v-if="askStore" v-model="store" label="Identificador da loja" autocomplete="organization" />
    <UiInput v-model="email" label="E-mail" type="email" inputmode="email" autocomplete="username" />
    <UiButton type="submit" class="w-full" :disabled="busy">Enviar instruções</UiButton>
  </form>
</template>
