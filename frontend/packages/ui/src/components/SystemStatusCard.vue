<script setup lang="ts">
import { computed } from 'vue'

import UiAlert from './UiAlert.vue'
import UiBadge, { type BadgeTone } from './UiBadge.vue'
import UiButton from './UiButton.vue'
import UiSkeleton from './UiSkeleton.vue'

export type SystemCardState = 'loading' | 'ok' | 'degraded' | 'unavailable' | 'error'

const props = defineProps<{
  state: SystemCardState
  version?: string
  errorMessage?: string
  correlationId?: string
}>()

defineEmits<{ retry: [] }>()

const content: Record<Exclude<SystemCardState, 'loading' | 'error'>, { tone: BadgeTone; label: string; text: string }> = {
  ok: { tone: 'success', label: 'Operacional', text: 'A API e o banco de dados estão respondendo normalmente.' },
  degraded: { tone: 'warning', label: 'Degradado', text: 'A plataforma responde, mas um serviço de apoio está indisponível.' },
  unavailable: { tone: 'danger', label: 'Indisponível', text: 'A plataforma não consegue atender requisições no momento.' },
}

const current = computed(() => (props.state === 'loading' || props.state === 'error' ? null : content[props.state]))
</script>

<template>
  <section class="rounded-card border border-line bg-surface p-5 sm:p-6" aria-labelledby="system-status-title" :aria-busy="state === 'loading'">
    <h2 id="system-status-title" class="text-sm font-semibold text-muted">Estado da plataforma</h2>

    <div v-if="state === 'loading'" class="mt-4 space-y-3" role="status">
      <span class="sr-only">Consultando o estado da plataforma</span>
      <UiSkeleton class="h-7 w-32" />
      <UiSkeleton class="h-4 w-full max-w-sm" />
    </div>

    <div v-else-if="state === 'error'" class="mt-4 space-y-4">
      <UiAlert title="Não foi possível consultar a plataforma">
        <p>{{ errorMessage }}</p>
        <p v-if="correlationId" class="mt-1 break-all">Código para suporte: {{ correlationId }}</p>
      </UiAlert>
      <UiButton @click="$emit('retry')">Tentar novamente</UiButton>
    </div>

    <div v-else-if="current" class="mt-4 space-y-3" role="status">
      <UiBadge :tone="current.tone">{{ current.label }}</UiBadge>
      <p class="text-base leading-relaxed text-ink">{{ current.text }}</p>
      <p v-if="version" class="text-sm text-muted">Versão {{ version }}</p>
    </div>
  </section>
</template>
