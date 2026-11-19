<script setup lang="ts">
import type { AccessEvent, MfaEnrollment, SessionView } from '@whitelabel/api-client'
import { UiAlert, UiBadge, UiButton, UiInput, UiSkeleton } from '@whitelabel/ui'
import { onMounted, ref } from 'vue'

import { errorMessage, eventLabel, formatDateTime } from '../messages'
import type { AuthStore } from '../store'
import RecoveryCodes from './RecoveryCodes.vue'
import TotpSetup from './TotpSetup.vue'

const props = withDefaults(defineProps<{ auth: AuthStore; mfaMandatory?: boolean }>(), { mfaMandatory: false })

const client = () => props.auth.client()

const sessions = ref<SessionView[]>()
const history = ref<AccessEvent[]>()
const failure = ref<string>()
const notice = ref<string>()

const currentPassword = ref('')
const newPassword = ref('')

const mfaPassword = ref('')
const mfaCode = ref('')
const enrollment = ref<MfaEnrollment>()
const recoveryCodes = ref<string[]>([])

async function run(action: () => Promise<void>, success?: string) {
  failure.value = undefined
  notice.value = undefined
  try {
    await action()
    notice.value = success
  } catch (error) {
    failure.value = errorMessage(error)
  }
}

async function loadActivity() {
  ;[sessions.value, history.value] = await Promise.all([client().listSessions(), client().accessHistory()])
}

onMounted(() => run(loadActivity))

const changePassword = () =>
  run(async () => {
    await client().changePassword(currentPassword.value, newPassword.value)
    currentPassword.value = ''
    newPassword.value = ''
    await loadActivity()
  }, 'Senha alterada. As outras sessões foram encerradas.')

const startEnrollment = () =>
  run(async () => {
    enrollment.value = await client().enrollMfa(mfaPassword.value)
    mfaPassword.value = ''
  })

const confirmEnrollment = () =>
  run(async () => {
    recoveryCodes.value = (await client().confirmMfa(mfaCode.value.trim())).recoveryCodes
    mfaCode.value = ''
    enrollment.value = undefined
    await props.auth.reload()
    await loadActivity()
  })

const disableMfa = () =>
  run(async () => {
    await client().disableMfa(mfaPassword.value, mfaCode.value.trim())
    mfaPassword.value = ''
    mfaCode.value = ''
    await props.auth.reload()
  }, 'Verificação em duas etapas desativada.')

const revoke = (id: string) =>
  run(async () => {
    await client().revokeSession(id)
    await loadActivity()
  })

const revokeOthers = () =>
  run(async () => {
    await client().revokeOtherSessions()
    await loadActivity()
  }, 'As outras sessões foram encerradas.')
</script>

<template>
  <div class="space-y-6">
    <UiAlert v-if="failure" :title="failure" />
    <p v-if="notice" role="status" class="rounded-control bg-success-soft px-4 py-3 text-sm font-semibold text-success">{{ notice }}</p>

    <section class="space-y-4 rounded-card border border-line bg-surface p-5 sm:p-6" aria-labelledby="mfa-title">
      <div class="flex flex-wrap items-center justify-between gap-3">
        <h2 id="mfa-title" class="font-display text-2xl text-ink">Verificação em duas etapas</h2>
        <UiBadge :tone="auth.user?.mfaEnabled ? 'success' : 'warning'">{{ auth.user?.mfaEnabled ? 'Ativa' : 'Inativa' }}</UiBadge>
      </div>

      <RecoveryCodes v-if="recoveryCodes.length" :codes="recoveryCodes" @done="recoveryCodes = []" />

      <form v-else-if="enrollment" class="max-w-md space-y-4" @submit.prevent="confirmEnrollment">
        <TotpSetup :uri="enrollment.otpauthUri" :secret="enrollment.secret" />
        <UiInput v-model="mfaCode" label="Código de verificação" inputmode="numeric" autocomplete="one-time-code" />
        <UiButton type="submit">Ativar</UiButton>
      </form>

      <form v-else-if="!auth.user?.mfaEnabled" class="max-w-md space-y-4" @submit.prevent="startEnrollment">
        <p class="text-sm text-muted">Proteja sua conta com um código gerado no celular.</p>
        <UiInput v-model="mfaPassword" label="Confirme sua senha" type="password" autocomplete="current-password" />
        <UiButton type="submit">Configurar</UiButton>
      </form>

      <p v-else-if="mfaMandatory" class="text-sm text-muted">A verificação em duas etapas é obrigatória nesta conta.</p>

      <form v-else class="max-w-md space-y-4" @submit.prevent="disableMfa">
        <UiInput v-model="mfaPassword" label="Confirme sua senha" type="password" autocomplete="current-password" />
        <UiInput v-model="mfaCode" label="Código de verificação" autocomplete="one-time-code" />
        <UiButton type="submit">Desativar</UiButton>
      </form>
    </section>

    <section class="space-y-4 rounded-card border border-line bg-surface p-5 sm:p-6" aria-labelledby="password-title">
      <h2 id="password-title" class="font-display text-2xl text-ink">Senha</h2>
      <form class="max-w-md space-y-4" @submit.prevent="changePassword">
        <UiInput v-model="currentPassword" label="Senha atual" type="password" autocomplete="current-password" />
        <UiInput v-model="newPassword" label="Nova senha" type="password" autocomplete="new-password" hint="Use pelo menos 10 caracteres." />
        <UiButton type="submit">Alterar senha</UiButton>
      </form>
    </section>

    <section class="space-y-4 rounded-card border border-line bg-surface p-5 sm:p-6" aria-labelledby="sessions-title">
      <div class="flex flex-wrap items-center justify-between gap-3">
        <h2 id="sessions-title" class="font-display text-2xl text-ink">Dispositivos conectados</h2>
        <UiButton v-if="(sessions?.length ?? 0) > 1" @click="revokeOthers">Encerrar as outras</UiButton>
      </div>
      <UiSkeleton v-if="!sessions" class="h-16 w-full" />
      <ul v-else class="divide-y divide-line">
        <li v-for="session in sessions" :key="session.id" class="flex flex-wrap items-center justify-between gap-3 py-3">
          <div>
            <p class="font-semibold text-ink">{{ session.device }}</p>
            <p class="text-sm text-muted">{{ session.ip }} · último uso em {{ formatDateTime(session.lastUsedAt) }}</p>
          </div>
          <UiBadge v-if="session.current" tone="success">Este dispositivo</UiBadge>
          <UiButton v-else @click="revoke(session.id)">Encerrar</UiButton>
        </li>
      </ul>
    </section>

    <section class="space-y-4 rounded-card border border-line bg-surface p-5 sm:p-6" aria-labelledby="history-title">
      <h2 id="history-title" class="font-display text-2xl text-ink">Histórico de acessos</h2>
      <UiSkeleton v-if="!history" class="h-16 w-full" />
      <ul v-else class="divide-y divide-line">
        <li v-for="event in history" :key="event.id" class="py-3">
          <p class="font-semibold text-ink">{{ eventLabel(event.event) }}</p>
          <p class="text-sm text-muted">{{ event.device }} · {{ event.ip }} · {{ formatDateTime(event.occurredAt) }}</p>
        </li>
      </ul>
    </section>
  </div>
</template>
