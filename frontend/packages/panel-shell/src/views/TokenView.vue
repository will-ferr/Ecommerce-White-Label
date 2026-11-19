<script setup lang="ts">
import { PasswordTokenForm } from '@whitelabel/auth'
import { UiAlert } from '@whitelabel/ui'
import { computed } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'

import { useAuth } from '../context'

const props = defineProps<{ mode: 'reset' | 'invitation' }>()

const auth = useAuth()
const route = useRoute()
const router = useRouter()

const token = computed(() => (typeof route.query.token === 'string' ? route.query.token : ''))
const heading = computed(() => (props.mode === 'invitation' ? 'Aceitar convite' : 'Criar nova senha'))
</script>

<template>
  <div class="mx-auto max-w-md space-y-6">
    <h1 class="font-display text-3xl text-ink">{{ heading }}</h1>
    <div class="rounded-card border border-line bg-surface p-5 sm:p-6">
      <PasswordTokenForm v-if="token" :auth="auth" :token="token" :mode="mode" @done="router.replace({ name: 'login' })" />
      <UiAlert v-else title="Este link é inválido ou já expirou." />
    </div>
    <RouterLink to="/entrar" class="inline-flex min-h-11 items-center font-semibold text-primary underline underline-offset-4">Voltar para entrar</RouterLink>
  </div>
</template>
