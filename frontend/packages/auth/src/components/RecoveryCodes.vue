<script setup lang="ts">
import { UiButton } from '@whitelabel/ui'

defineProps<{ codes: string[] }>()
defineEmits<{ done: [] }>()
</script>

<template>
  <div class="space-y-4">
    <div>
      <h2 class="font-display text-2xl text-ink">Guarde seus códigos de recuperação</h2>
      <p class="mt-1 text-sm text-muted">
        Cada código funciona uma única vez, caso você perca o aplicativo. Eles não serão exibidos novamente.
      </p>
    </div>
    <ul class="grid gap-2 rounded-control border border-line p-4 font-mono text-sm text-ink sm:grid-cols-2" data-testid="recovery-codes">
      <li v-for="code in codes" :key="code">{{ code }}</li>
    </ul>
    <UiButton @click="$emit('done')">Já guardei os códigos</UiButton>
  </div>
</template>
