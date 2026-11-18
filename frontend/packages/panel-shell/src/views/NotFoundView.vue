<script setup lang="ts">
import { RouterLink } from 'vue-router'
</script>

<template>
  <div class="space-y-4">
    <h1 class="font-display text-3xl text-ink">Página não encontrada</h1>
    <p class="text-muted">O endereço acessado não existe neste painel.</p>
    <RouterLink to="/" class="font-semibold text-primary underline underline-offset-4">Voltar ao início</RouterLink>
  </div>
</template>
