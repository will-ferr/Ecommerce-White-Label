<script setup lang="ts">
import { ForgotPasswordForm } from '@whitelabel/auth'
import { RouterLink } from 'vue-router'

import { useAuth, usePanel } from '../context'

const auth = useAuth()
const panel = usePanel()
</script>

<template>
  <div class="mx-auto max-w-md space-y-6">
    <h1 class="font-display text-3xl text-ink">Recuperar senha</h1>
    <div class="rounded-card border border-line bg-surface p-5 sm:p-6">
      <ForgotPasswordForm :auth="auth" :ask-store="panel.portal === 'staff'" />
    </div>
    <RouterLink to="/entrar" class="inline-flex min-h-11 items-center font-semibold text-primary underline underline-offset-4">Voltar para entrar</RouterLink>
  </div>
</template>
