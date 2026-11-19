<script setup lang="ts">
import { LoginFlow } from '@whitelabel/auth'

const auth = useStoreAuth()
</script>

<template>
  <StoreAuthPage heading="Entrar">
    <LoginFlow :auth="auth" @authenticated="navigateTo('/conta')" />
    <template #links>
      <div class="flex flex-wrap justify-between gap-x-6">
        <NuxtLink to="/cadastro" class="inline-flex min-h-11 items-center font-semibold text-primary underline underline-offset-4">Criar conta</NuxtLink>
        <NuxtLink to="/recuperar-senha" class="inline-flex min-h-11 items-center font-semibold text-primary underline underline-offset-4">Esqueci minha senha</NuxtLink>
      </div>
    </template>
  </StoreAuthPage>
</template>
