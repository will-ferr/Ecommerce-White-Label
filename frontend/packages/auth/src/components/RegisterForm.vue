<script setup lang="ts">
import { UiAlert, UiButton, UiInput } from '@whitelabel/ui'
import { ref } from 'vue'

import { errorMessage } from '../messages'
import type { AuthStore } from '../store'

const props = defineProps<{ auth: AuthStore }>()

const name = ref('')
const email = ref('')
const password = ref('')
const sent = ref(false)
const failure = ref<string>()
const busy = ref(false)

async function submit() {
  busy.value = true
  failure.value = undefined
  try {
    await props.auth.client().register({ name: name.value, email: email.value, password: password.value })
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
    Cadastro recebido. Enviamos uma mensagem para {{ email }} com os próximos passos.
  </p>
  <form v-else class="space-y-4" @submit.prevent="submit">
    <UiAlert v-if="failure" :title="failure" />
    <UiInput v-model="name" label="Nome" autocomplete="name" />
    <UiInput v-model="email" label="E-mail" type="email" inputmode="email" autocomplete="email" />
    <UiInput v-model="password" label="Senha" type="password" autocomplete="new-password" hint="Use pelo menos 10 caracteres. Frases longas são mais seguras." />
    <UiButton type="submit" class="w-full" :disabled="busy">Criar conta</UiButton>
  </form>
</template>
