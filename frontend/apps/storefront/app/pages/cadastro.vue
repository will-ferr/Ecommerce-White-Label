<script setup lang="ts">
import { RegisterForm } from '@whitelabel/auth'

const auth = useStoreAuth()
</script>

<template>
  <StoreAuthPage heading="Criar conta">
    <RegisterForm :auth="auth" />
    <template #links>
      <NuxtLink to="/entrar" class="inline-flex min-h-11 items-center font-semibold text-primary underline underline-offset-4">Já tenho conta</NuxtLink>
    </template>
  </StoreAuthPage>
</template>
