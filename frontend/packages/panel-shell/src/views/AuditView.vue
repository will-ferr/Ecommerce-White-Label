<script setup lang="ts">
import type { AuditEntry } from '@whitelabel/api-client'
import { errorMessage, formatDateTime } from '@whitelabel/auth'
import { UiAlert, UiSkeleton } from '@whitelabel/ui'
import { onMounted, ref } from 'vue'

import { useAuth } from '../context'

const auth = useAuth()
const entries = ref<AuditEntry[]>()
const failure = ref<string>()

const ACTION_LABELS: Record<string, string> = {
  'identity.member.invited': 'Pessoa convidada',
  'identity.member.role_changed': 'Papel alterado',
  'identity.member.deactivated': 'Acesso removido',
  'identity.invitation.accepted': 'Convite aceito',
  'identity.password.changed': 'Senha alterada',
  'identity.password.reset': 'Senha redefinida',
  'identity.mfa.enabled': 'Verificação em duas etapas ativada',
  'identity.mfa.disabled': 'Verificação em duas etapas desativada',
  'identity.mfa.recovery_codes_replaced': 'Códigos de recuperação substituídos',
  'identity.session.reuse_detected': 'Possível roubo de sessão bloqueado',
  'identity.user.registered': 'Conta criada',
}

onMounted(async () => {
  try {
    entries.value = await auth.client().listAudit()
  } catch (error) {
    failure.value = errorMessage(error)
  }
})
</script>

<template>
  <div class="space-y-6">
    <h1 class="font-display text-3xl text-ink sm:text-4xl">Auditoria</h1>
    <UiAlert v-if="failure" :title="failure" />
    <section v-else class="rounded-card border border-line bg-surface p-5 sm:p-6" aria-label="Operações recentes">
      <UiSkeleton v-if="!entries" class="h-24 w-full" />
      <p v-else-if="entries.length === 0" class="text-muted">Nenhuma operação registrada até agora.</p>
      <ul v-else class="divide-y divide-line">
        <li v-for="entry in entries" :key="entry.id" class="py-3">
          <p class="font-semibold text-ink">{{ ACTION_LABELS[entry.action] ?? entry.action }}</p>
          <p class="text-sm break-all text-muted">
            {{ formatDateTime(entry.occurredAt) }} · {{ entry.actorType === 'operator' ? 'Operador da plataforma' : entry.actorId }}
            <template v-if="entry.ip"> · {{ entry.ip }}</template>
          </p>
        </li>
      </ul>
    </section>
  </div>
</template>
