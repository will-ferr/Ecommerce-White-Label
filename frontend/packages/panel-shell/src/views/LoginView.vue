<script setup lang="ts">
import { LoginFlow } from '@whitelabel/auth'
import { RouterLink, useRoute, useRouter } from 'vue-router'

import { useAuth, usePanel } from '../context'
import { safeRedirect } from '../index'

const auth = useAuth()
const panel = usePanel()
const route = useRoute()
const router = useRouter()

const proceed = () => router.replace(safeRedirect(route.query.redirect))
</script>

<template>
  <div class="mx-auto max-w-md space-y-6">
    <h1 class="font-display text-3xl text-ink">Entrar</h1>
    <div class="rounded-card border border-line bg-surface p-5 sm:p-6">
      <LoginFlow :auth="auth" :ask-store="panel.portal === 'staff'" @authenticated="proceed" />
    </div>
    <RouterLink to="/recuperar-senha" class="inline-flex min-h-11 items-center font-semibold text-primary underline underline-offset-4">
      Esqueci minha senha
    </RouterLink>
  </div>
</template>
