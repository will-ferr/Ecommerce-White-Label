<script setup lang="ts">
import { SystemStatusCard } from '@whitelabel/ui'
import { onMounted } from 'vue'

import { useSystemStore } from '../stores/system'

defineProps<{ heading: string; description: string }>()

const system = useSystemStore()

onMounted(() => system.load())
</script>

<template>
  <div class="space-y-8">
    <div class="space-y-3">
      <h1 class="font-display text-3xl leading-tight text-ink sm:text-4xl">{{ heading }}</h1>
      <p class="max-w-prose text-base leading-relaxed text-muted">{{ description }}</p>
    </div>
    <SystemStatusCard
      :state="system.state"
      :version="system.version"
      :error-message="system.errorMessage"
      :correlation-id="system.correlationId"
      @retry="system.load()"
    />
  </div>
</template>
