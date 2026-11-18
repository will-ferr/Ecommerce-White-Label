<script setup lang="ts">
import { SystemStatusCard } from '@whitelabel/ui'

useSeoMeta({
  title: 'Loja em preparação',
  description: 'Esta loja ainda está em preparação.',
  robots: 'noindex',
})

const client = useApiClient()
const system = useSystemStore()

await callOnce(() => system.load(client))
</script>

<template>
  <main class="mx-auto flex min-h-dvh max-w-3xl flex-col justify-center gap-8 px-4 py-12 sm:px-6">
    <div class="space-y-3">
      <h1 class="font-display text-4xl leading-tight text-ink sm:text-5xl">Loja em preparação</h1>
      <p class="max-w-prose text-base leading-relaxed text-muted">
        O catálogo e a identidade visual de cada loja serão adicionados nas próximas fases.
      </p>
    </div>
    <SystemStatusCard
      :state="system.state"
      :version="system.version"
      :error-message="system.errorMessage"
      :correlation-id="system.correlationId"
      @retry="system.load(client)"
    />
  </main>
</template>
