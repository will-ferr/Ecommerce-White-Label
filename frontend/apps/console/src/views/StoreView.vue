<script setup lang="ts">
import { ApiError, type StoreDetail } from '@whitelabel/api-client'
import { errorMessage } from '@whitelabel/auth'
import { useAuth } from '@whitelabel/panel-shell'
import { UiAlert, UiBadge, UiButton, UiInput, UiSkeleton } from '@whitelabel/ui'
import { computed, onMounted, reactive, ref, useId } from 'vue'
import { RouterLink } from 'vue-router'

import { FEATURE_LABELS, formatDate, STATUS_LABELS, useConsole } from '../console'

const props = defineProps<{ slug: string }>()

const api = useConsole()
const auth = useAuth()
const store = ref<StoreDetail>()
const failure = ref<string>()
const notice = ref<string>()
const host = ref('')
const roleId = useId()
const invite = reactive({ name: '', email: '', role: 'owner' })
const canManage = computed(() => auth.can('stores:manage'))

const ROLES: Record<string, string> = { owner: 'Proprietário', manager: 'Gerente', support: 'Atendimento', viewer: 'Leitura' }

async function load() {
  store.value = await api.store(props.slug)
}

async function run(action: () => Promise<unknown>, success: string, messages: Record<string, string> = {}) {
  failure.value = undefined
  notice.value = undefined
  try {
    await action()
    notice.value = success
    await load()
    return true
  } catch (error) {
    failure.value = error instanceof ApiError && messages[error.code] ? messages[error.code] : errorMessage(error)
    return false
  }
}

onMounted(async () => {
  try {
    await load()
  } catch (error) {
    failure.value = errorMessage(error)
  }
})

function toggleStatus() {
  if (!store.value) return
  const suspending = store.value.status === 'active'
  if (suspending && !window.confirm(`Suspender a loja ${store.value.name}? Ela deixa de responder em até 30 segundos.`)) return
  void run(() => api.setStatus(props.slug, suspending ? 'suspended' : 'active'), suspending ? 'Loja suspensa.' : 'Loja reativada.')
}

async function addDomain() {
  const ok = await run(() => api.addDomain(props.slug, host.value), 'Domínio adicionado. Crie o registro TXT abaixo e verifique.', {
    DOMAIN_ALREADY_EXISTS: 'Este domínio já está cadastrado.', INVALID_HOST: 'Informe um domínio válido.',
  })
  if (ok) host.value = ''
}

async function sendInvitation() {
  const ok = await run(() => api.invite(props.slug, { ...invite }), `Convite enviado para ${invite.email}.`, {
    MEMBER_ALREADY_EXISTS: 'Este e-mail já faz parte da equipe da loja.', VALIDATION_FAILED: 'Informe um nome e um e-mail válidos.',
  })
  if (ok) Object.assign(invite, { name: '', email: '', role: 'owner' })
}
</script>

<template>
  <div class="space-y-6">
    <RouterLink to="/lojas" class="inline-flex min-h-11 items-center text-sm font-semibold text-primary underline underline-offset-4">Voltar para lojas</RouterLink>
    <UiAlert v-if="failure" :title="failure" />
    <p v-if="notice" role="status" class="rounded-control bg-success-soft px-4 py-3 text-sm font-semibold text-success">{{ notice }}</p>
    <UiSkeleton v-if="!store && !failure" class="h-64 w-full" />

    <template v-if="store">
      <div class="flex flex-wrap items-center justify-between gap-4">
        <div>
          <h1 class="font-display text-3xl text-ink sm:text-4xl">{{ store.name }}</h1>
          <p class="text-muted">Plano {{ store.plan }} · {{ store.currency }} · criada em {{ formatDate(store.createdAt) }}</p>
        </div>
        <div class="flex items-center gap-3">
          <UiBadge :tone="store.status === 'active' ? 'success' : 'danger'" data-testid="store-status">{{ STATUS_LABELS[store.status] }}</UiBadge>
          <UiButton v-if="canManage" @click="toggleStatus">{{ store.status === 'active' ? 'Suspender loja' : 'Reativar loja' }}</UiButton>
        </div>
      </div>

      <section aria-labelledby="team-title" class="space-y-4 rounded-card border border-line bg-surface p-5 sm:p-6">
        <h2 id="team-title" class="text-lg font-semibold text-ink">Convidar para a equipe da loja</h2>
        <p class="text-sm text-muted">A pessoa recebe o convite por e-mail e define a própria senha. O link não aparece aqui.</p>
        <form v-if="canManage" class="grid gap-4 sm:grid-cols-3" @submit.prevent="sendInvitation">
          <UiInput v-model="invite.name" label="Nome" />
          <UiInput v-model="invite.email" label="E-mail" type="email" inputmode="email" />
          <div class="space-y-1.5">
            <label :for="roleId" class="block text-sm font-semibold text-ink">Papel</label>
            <select :id="roleId" v-model="invite.role" class="block min-h-11 w-full rounded-control border border-muted bg-surface px-3 text-ink">
              <option v-for="(label, value) in ROLES" :key="value" :value="value">{{ label }}</option>
            </select>
          </div>
          <div class="sm:col-span-3"><UiButton type="submit">Enviar convite</UiButton></div>
        </form>
      </section>

      <section aria-labelledby="domains-title" class="space-y-4 rounded-card border border-line bg-surface p-5 sm:p-6">
        <h2 id="domains-title" class="text-lg font-semibold text-ink">Domínios</h2>
        <ul class="divide-y divide-line">
          <li v-for="domain in store.domains" :key="domain.host" class="space-y-2 py-3">
            <div class="flex flex-wrap items-center justify-between gap-3">
              <span class="break-all font-semibold text-ink">{{ domain.host }}</span>
              <div class="flex flex-wrap items-center gap-3">
                <UiBadge :tone="domain.verifiedAt ? 'success' : 'warning'">{{ domain.verifiedAt ? 'Ativo' : 'Aguardando verificação' }}</UiBadge>
                <template v-if="canManage && domain.kind === 'custom'">
                  <UiButton v-if="!domain.verifiedAt" @click="run(() => api.verifyDomain(slug, domain.host), 'Domínio verificado.', { DOMAIN_NOT_VERIFIED: 'O registro TXT ainda não foi encontrado. A propagação do DNS pode levar algumas horas.' })">
                    Verificar<span class="sr-only"> {{ domain.host }}</span>
                  </UiButton>
                  <UiButton @click="run(() => api.removeDomain(slug, domain.host), 'Domínio removido.')">Remover<span class="sr-only"> {{ domain.host }}</span></UiButton>
                </template>
              </div>
            </div>
            <dl v-if="domain.verificationName" class="grid gap-1 rounded-control bg-canvas p-3 text-sm">
              <div><dt class="inline text-muted">Registro TXT: </dt><dd class="inline break-all font-mono text-ink">{{ domain.verificationName }}</dd></div>
              <div><dt class="inline text-muted">Valor: </dt><dd class="inline break-all font-mono text-ink">{{ domain.verificationValue }}</dd></div>
            </dl>
          </li>
        </ul>
        <form v-if="canManage" class="flex flex-wrap items-end gap-3" @submit.prevent="addDomain">
          <UiInput v-model="host" label="Domínio próprio" hint="Por exemplo: www.minhaloja.com.br" />
          <UiButton type="submit">Adicionar domínio</UiButton>
        </form>
      </section>

      <section aria-labelledby="features-title" class="space-y-4 rounded-card border border-line bg-surface p-5 sm:p-6">
        <h2 id="features-title" class="text-lg font-semibold text-ink">Recursos</h2>
        <ul class="divide-y divide-line">
          <li v-for="feature in store.features" :key="feature.key" class="flex flex-wrap items-center justify-between gap-3 py-3">
            <span class="text-ink">{{ FEATURE_LABELS[feature.key] ?? feature.key }}</span>
            <span v-if="!feature.inPlan" class="text-sm text-muted">Fora do plano</span>
            <label v-else class="flex min-h-11 items-center gap-3 text-ink">
              <input
                type="checkbox" class="size-5 accent-[var(--wl-color-primary)]" :checked="feature.enabled" :disabled="!canManage"
                @change="run(() => api.setFeature(slug, feature.key, ($event.target as HTMLInputElement).checked), 'Recurso atualizado.')"
              />
              Ativo
              <span class="sr-only">: {{ FEATURE_LABELS[feature.key] ?? feature.key }}</span>
            </label>
          </li>
        </ul>
      </section>
    </template>
  </div>
</template>
