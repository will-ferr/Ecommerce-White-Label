<script setup lang="ts">
import { ForgotPasswordForm } from '@whitelabel/auth'

const auth = useStoreAuth()
</script>

<template>
  <StoreAuthPage heading="Recuperar senha">
    <ForgotPasswordForm :auth="auth" />
    <template #links>
      <NuxtLink to="/entrar" class="inline-flex min-h-11 items-center font-semibold text-primary underline underline-offset-4">Voltar para entrar</NuxtLink>
    </template>
  </StoreAuthPage>
</template>
