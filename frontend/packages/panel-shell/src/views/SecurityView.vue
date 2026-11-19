<script setup lang="ts">
import { AccountSecurity } from '@whitelabel/auth'

import { useAuth, usePanel } from '../context'

const auth = useAuth()
const panel = usePanel()
</script>

<template>
  <div class="space-y-6">
    <h1 class="font-display text-3xl text-ink sm:text-4xl">Segurança da conta</h1>
    <AccountSecurity :auth="auth" :mfa-mandatory="panel.portal === 'platform'" />
  </div>
</template>
