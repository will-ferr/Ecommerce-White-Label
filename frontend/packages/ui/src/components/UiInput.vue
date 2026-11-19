<script setup lang="ts">
import { useId } from 'vue'

withDefaults(
  defineProps<{
    label: string
    type?: string
    autocomplete?: string
    inputmode?: 'text' | 'numeric' | 'email'
    required?: boolean
    error?: string
    hint?: string
  }>(),
  { type: 'text', autocomplete: 'off', inputmode: 'text', required: true, error: undefined, hint: undefined },
)

const model = defineModel<string>({ default: '' })
const id = useId()
</script>

<template>
  <div class="space-y-1.5">
    <label :for="id" class="block text-sm font-semibold text-ink">{{ label }}</label>
    <input
      :id="id"
      v-model="model"
      :type="type"
      :autocomplete="autocomplete"
      :inputmode="inputmode"
      :required="required"
      :aria-invalid="error ? 'true' : undefined"
      :aria-describedby="error || hint ? `${id}-help` : undefined"
      class="block min-h-11 w-full rounded-control border bg-surface px-3 text-base text-ink"
      :class="error ? 'border-danger' : 'border-muted'"
    />
    <p v-if="error || hint" :id="`${id}-help`" class="text-sm" :class="error ? 'text-danger' : 'text-muted'">
      {{ error ?? hint }}
    </p>
  </div>
</template>
