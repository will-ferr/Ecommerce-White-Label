<script setup lang="ts">
import { UiAlert, UiButton, UiInput } from '@whitelabel/ui'
import { computed, ref } from 'vue'

import { errorMessage } from '../messages'
import type { AuthStore } from '../store'

const props = defineProps<{ auth: AuthStore; token: string; mode: 'reset' | 'invitation' }>()
const emit = defineEmits<{ done: [] }>()

const name = ref('')
const password = ref('')
const confirmation = ref('')
const failure = ref<string>()
const busy = ref(false)
const mismatch = computed(() => confirmation.value !== '' && confirmation.value !== password.value)

async function submit() {
  if (password.value !== confirmation.value) return
  busy.value = true
  failure.value = undefined
  try {
    if (props.mode === 'invitation') {
      await props.auth.client().acceptInvitation({ token: props.token, name: name.value, password: password.value })
    } else {
      await props.auth.client().resetPassword(props.token, password.value)
    }
    emit('done')
  } catch (error) {
    failure.value = errorMessage(error)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <form class="space-y-4" @submit.prevent="submit">
    <UiAlert v-if="failure" :title="failure" />
    <UiInput v-if="mode === 'invitation'" v-model="name" label="Seu nome" autocomplete="name" />
    <UiInput v-model="password" label="Nova senha" type="password" autocomplete="new-password" hint="Use pelo menos 10 caracteres. Frases longas são mais seguras." />
    <UiInput
      v-model="confirmation"
      label="Confirme a nova senha"
      type="password"
      autocomplete="new-password"
      :error="mismatch ? 'As senhas não coincidem.' : undefined"
    />
    <UiButton type="submit" class="w-full" :disabled="busy || mismatch">Salvar senha</UiButton>
  </form>
</template>
