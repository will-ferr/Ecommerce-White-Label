<script setup lang="ts">
import type { AuthResult, MfaEnrollment } from '@whitelabel/api-client'
import { UiAlert, UiButton, UiInput } from '@whitelabel/ui'
import { ref } from 'vue'

import { errorMessage } from '../messages'
import type { AuthStore } from '../store'
import RecoveryCodes from './RecoveryCodes.vue'
import TotpSetup from './TotpSetup.vue'

const props = withDefaults(defineProps<{ auth: AuthStore; askStore?: boolean }>(), { askStore: false })
const emit = defineEmits<{ authenticated: [] }>()

type Step = 'credentials' | 'code' | 'setup' | 'recovery'

const step = ref<Step>('credentials')
const store = ref('')
const email = ref('')
const password = ref('')
const code = ref('')
const challenge = ref('')
const enrollment = ref<MfaEnrollment>()
const recoveryCodes = ref<string[]>([])
const failure = ref<string>()
const busy = ref(false)

async function attempt(action: () => Promise<void>) {
  busy.value = true
  failure.value = undefined
  try {
    await action()
  } catch (error) {
    failure.value = errorMessage(error)
  } finally {
    busy.value = false
  }
}

function finish(result: AuthResult) {
  props.auth.accept(result)
  password.value = ''
  if (result.recoveryCodes?.length) {
    recoveryCodes.value = result.recoveryCodes
    step.value = 'recovery'
    return
  }
  emit('authenticated')
}

const client = () => props.auth.client()

const submitCredentials = () =>
  attempt(async () => {
    const result = await client().login({
      email: email.value,
      password: password.value,
      ...(props.askStore ? { store: store.value.trim().toLowerCase() } : {}),
    })
    if (result.status === 'mfa_required') {
      challenge.value = result.challenge ?? ''
      step.value = 'code'
    } else if (result.status === 'mfa_enrollment_required') {
      challenge.value = result.challenge ?? ''
      enrollment.value = await client().startSetup(challenge.value)
      step.value = 'setup'
    } else {
      finish(result)
    }
  })

const submitCode = () =>
  attempt(async () => {
    const result =
      step.value === 'setup'
        ? await client().confirmSetup(challenge.value, code.value.trim())
        : await client().completeChallenge(challenge.value, code.value.trim())
    code.value = ''
    finish(result)
  })
</script>

<template>
  <div class="space-y-5">
    <UiAlert v-if="failure" :title="failure" />

    <form v-if="step === 'credentials'" class="space-y-4" @submit.prevent="submitCredentials">
      <UiInput v-if="askStore" v-model="store" label="Identificador da loja" autocomplete="organization" />
      <UiInput v-model="email" label="E-mail" type="email" inputmode="email" autocomplete="username" />
      <UiInput v-model="password" label="Senha" type="password" autocomplete="current-password" />
      <UiButton type="submit" class="w-full" :disabled="busy">{{ busy ? 'Entrando…' : 'Entrar' }}</UiButton>
    </form>

    <form v-else-if="step === 'code'" class="space-y-4" @submit.prevent="submitCode">
      <p class="text-sm text-muted">Digite o código do aplicativo autenticador ou um código de recuperação.</p>
      <UiInput v-model="code" label="Código de verificação" autocomplete="one-time-code" />
      <UiButton type="submit" class="w-full" :disabled="busy">Confirmar</UiButton>
    </form>

    <form v-else-if="step === 'setup' && enrollment" class="space-y-4" @submit.prevent="submitCode">
      <p class="text-sm font-semibold text-ink">Esta conta exige verificação em duas etapas.</p>
      <TotpSetup :uri="enrollment.otpauthUri" :secret="enrollment.secret" />
      <UiInput v-model="code" label="Código de verificação" inputmode="numeric" autocomplete="one-time-code" />
      <UiButton type="submit" class="w-full" :disabled="busy">Ativar e entrar</UiButton>
    </form>

    <RecoveryCodes v-else-if="step === 'recovery'" :codes="recoveryCodes" @done="emit('authenticated')" />
  </div>
</template>
