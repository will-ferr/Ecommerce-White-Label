<script setup lang="ts">
import type { RoleView, TeamMember } from '@whitelabel/api-client'
import { errorMessage } from '@whitelabel/auth'
import { UiAlert, UiBadge, UiButton, UiInput, UiSkeleton } from '@whitelabel/ui'
import { computed, onMounted, ref, useId } from 'vue'

import { useAuth } from '../context'

const auth = useAuth()
const client = () => auth.client()
const canManage = computed(() => auth.can('team:manage'))

const members = ref<TeamMember[]>()
const roles = ref<RoleView[]>([])
const failure = ref<string>()
const notice = ref<string>()

const name = ref('')
const email = ref('')
const role = ref('')
const roleFieldId = useId()

const STATUS_LABELS: Record<string, string> = { active: 'Ativo', invited: 'Convidado', disabled: 'Desativado' }
const STATUS_TONES = { active: 'success', invited: 'warning', disabled: 'danger' } as const

function toneOf(status: string) {
  return STATUS_TONES[status as keyof typeof STATUS_TONES] ?? 'warning'
}

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

async function load() {
  ;[members.value, roles.value] = await Promise.all([client().listTeam(), client().listRoles()])
  role.value ||= roles.value.at(-1)?.code ?? ''
}

onMounted(() => run(load))

const invite = () =>
  run(async () => {
    await client().inviteMember({ name: name.value, email: email.value, role: role.value })
    name.value = ''
    email.value = ''
    await load()
  }, 'Convite enviado por e-mail.')

const changeRole = (member: TeamMember, code: string) =>
  run(async () => {
    await client().changeRole(member.id, code)
    await load()
  }, 'Papel atualizado.')

const deactivate = (member: TeamMember) =>
  run(async () => {
    await client().deactivateMember(member.id)
    await load()
  }, 'Acesso removido.')
</script>

<template>
  <div class="space-y-6">
    <h1 class="font-display text-3xl text-ink sm:text-4xl">Equipe</h1>

    <UiAlert v-if="failure" :title="failure" />
    <p v-if="notice" role="status" class="rounded-control bg-success-soft px-4 py-3 text-sm font-semibold text-success">{{ notice }}</p>

    <section v-if="canManage" class="space-y-4 rounded-card border border-line bg-surface p-5 sm:p-6" aria-labelledby="invite-title">
      <h2 id="invite-title" class="font-display text-2xl text-ink">Convidar pessoa</h2>
      <form class="grid gap-4 sm:grid-cols-2" @submit.prevent="invite">
        <UiInput v-model="name" label="Nome" autocomplete="off" />
        <UiInput v-model="email" label="E-mail" type="email" inputmode="email" autocomplete="off" />
        <div class="space-y-1.5">
          <label :for="roleFieldId" class="block text-sm font-semibold text-ink">Papel</label>
          <select :id="roleFieldId" v-model="role" class="block min-h-11 w-full rounded-control border border-muted bg-surface px-3 text-base text-ink">
            <option v-for="option in roles" :key="option.code" :value="option.code">{{ option.name }}</option>
          </select>
        </div>
        <div class="flex items-end">
          <UiButton type="submit">Enviar convite</UiButton>
        </div>
      </form>
    </section>

    <section class="rounded-card border border-line bg-surface p-5 sm:p-6" aria-labelledby="members-title">
      <h2 id="members-title" class="font-display text-2xl text-ink">Pessoas</h2>
      <UiSkeleton v-if="!members" class="mt-4 h-24 w-full" />
      <ul v-else class="mt-2 divide-y divide-line">
        <li v-for="member in members" :key="member.id" class="flex flex-wrap items-center justify-between gap-3 py-4">
          <div class="min-w-0">
            <p class="font-semibold break-words text-ink">{{ member.name }}</p>
            <p class="text-sm break-all text-muted">{{ member.email }}</p>
          </div>
          <div class="flex flex-wrap items-center gap-3">
            <UiBadge :tone="toneOf(member.status)">{{ STATUS_LABELS[member.status] ?? member.status }}</UiBadge>
            <select
              v-if="canManage && member.id !== auth.user?.id && member.status !== 'disabled'"
              :value="member.role"
              :aria-label="`Papel de ${member.name}`"
              class="min-h-11 rounded-control border border-muted bg-surface px-3 text-sm text-ink"
              @change="changeRole(member, ($event.target as HTMLSelectElement).value)"
            >
              <option v-for="option in roles" :key="option.code" :value="option.code">{{ option.name }}</option>
            </select>
            <span v-else class="text-sm text-muted">{{ member.roleName }}</span>
            <UiButton v-if="canManage && member.id !== auth.user?.id && member.status !== 'disabled'" @click="deactivate(member)">
              Remover acesso
            </UiButton>
          </div>
        </li>
      </ul>
    </section>
  </div>
</template>
