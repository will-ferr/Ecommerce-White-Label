<script setup lang="ts">
import { PasswordTokenForm } from '@whitelabel/auth'
import { UiAlert } from '@whitelabel/ui'

const auth = useStoreAuth()
const route = useRoute()
const token = computed(() => (typeof route.query.token === 'string' ? route.query.token : ''))
</script>

<template>
  <StoreAuthPage heading="Criar nova senha">
    <PasswordTokenForm v-if="token" :auth="auth" :token="token" mode="reset" @done="navigateTo('/entrar')" />
    <UiAlert v-else title="Este link é inválido ou já expirou." />
  </StoreAuthPage>
</template>
