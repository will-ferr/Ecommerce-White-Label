<script setup lang="ts">
import { ApiError, type StoreTheme, type ThemeTokens } from '@whitelabel/api-client'
import { errorMessage } from '@whitelabel/auth'
import { UiAlert, UiBadge, UiButton, UiInput, UiSkeleton } from '@whitelabel/ui'
import { computed, onMounted, ref, useId } from 'vue'

import { formatDate, useManagement } from '../commerce'

const api = useManagement()
const theme = ref<StoreTheme>()
const tokens = ref<ThemeTokens>()
const failure = ref<string>()
const published = ref<string>()
const errors = ref<Record<string, string>>({})
const busy = ref(false)
const ids = { body: useId(), display: useId(), radius: useId(), density: useId(), header: useId() }

type ColorKey = keyof ThemeTokens['colors']
const COLORS: { key: ColorKey; label: string }[] = [
  { key: 'canvas', label: 'Fundo da página' },
  { key: 'surface', label: 'Fundo dos cartões' },
  { key: 'ink', label: 'Texto principal' },
  { key: 'muted', label: 'Texto secundário' },
  { key: 'line', label: 'Linhas e bordas' },
  { key: 'primary', label: 'Cor principal' },
  { key: 'primaryStrong', label: 'Cor principal ao passar o mouse' },
  { key: 'onPrimary', label: 'Texto sobre a cor principal' },
]
const FONTS: Record<string, string> = { system: 'Sistema', humanist: 'Humanista', geometric: 'Geométrica', serif: 'Serifada', slab: 'Slab' }
const RADIUS: Record<string, string> = { none: 'Reto', small: 'Pequeno', medium: 'Médio', large: 'Grande' }
const RADIUS_PX: Record<string, string> = { none: '0px', small: '6px', medium: '12px', large: '24px' }
const DENSITY: Record<string, string> = { compact: 'Compacto', comfortable: 'Confortável', spacious: 'Espaçoso' }
const HEADERS: Record<string, string> = { classic: 'Nome à esquerda', centered: 'Nome centralizado' }

async function load() {
  theme.value = await api.theme()
  // A plain copy: the form edits it without touching the published tokens.
  tokens.value = JSON.parse(JSON.stringify(theme.value.tokens)) as ThemeTokens
}

onMounted(async () => {
  try {
    await load()
  } catch (error) {
    failure.value = errorMessage(error)
  }
})

const preview = computed(() => {
  if (!tokens.value) return {}
  const c = tokens.value.colors
  return {
    '--p-canvas': c.canvas, '--p-surface': c.surface, '--p-ink': c.ink, '--p-muted': c.muted, '--p-line': c.line,
    '--p-primary': c.primary, '--p-on-primary': c.onPrimary, '--p-radius': RADIUS_PX[tokens.value.shape.radius] ?? '12px',
  }
})

function describe(error: ApiError) {
  return Object.fromEntries(error.fieldErrors.map((field) => [
    field.field,
    field.code === 'low_contrast' ? 'Contraste insuficiente para leitura (mínimo 4,5:1).'
      : field.field.startsWith('assets.') ? 'Use um endereço HTTPS completo.' : 'Valor inválido.',
  ]))
}

async function publish() {
  if (!tokens.value) return
  busy.value = true
  failure.value = undefined
  published.value = undefined
  errors.value = {}
  try {
    const { version } = await api.publishTheme(tokens.value)
    published.value = `Versão ${version} publicada. A loja já usa a nova aparência.`
    await load()
  } catch (error) {
    if (error instanceof ApiError && error.fieldErrors.length) {
      errors.value = describe(error)
      failure.value = 'A aparência não foi publicada. Confira os campos destacados.'
    } else {
      failure.value = errorMessage(error)
    }
  } finally {
    busy.value = false
  }
}

async function rollback(version: number) {
  failure.value = undefined
  published.value = undefined
  try {
    await api.rollbackTheme(version)
    published.value = `A versão ${version} voltou a ser usada pela loja.`
    await load()
  } catch (error) {
    failure.value = errorMessage(error)
  }
}
</script>

<template>
  <div class="space-y-6">
    <h1 class="font-display text-3xl text-ink sm:text-4xl">Aparência</h1>
    <p class="max-w-prose text-muted">As cores passam por uma verificação de contraste antes de publicar, para a loja continuar legível.</p>
    <UiAlert v-if="failure" :title="failure" />
    <p v-if="published" role="status" class="rounded-control bg-success-soft px-4 py-3 text-sm font-semibold text-success">{{ published }}</p>
    <UiSkeleton v-if="!tokens && !failure" class="h-96 w-full" />

    <div v-if="tokens && theme" class="grid gap-6 lg:grid-cols-[1fr_20rem]">
      <form class="space-y-6 rounded-card border border-line bg-surface p-5 sm:p-6" @submit.prevent="publish">
        <fieldset class="grid gap-4 sm:grid-cols-2">
          <legend class="mb-2 text-lg font-semibold text-ink">Cores</legend>
          <div v-for="color in COLORS" :key="color.key" class="flex items-end gap-3">
            <input
              v-model="tokens.colors[color.key]" type="color" :aria-label="`${color.label}: seletor`"
              class="h-11 w-12 shrink-0 cursor-pointer rounded-control border border-muted bg-surface"
            />
            <div class="min-w-0 flex-1">
              <UiInput v-model="tokens.colors[color.key]" :label="color.label" :error="errors[`colors.${color.key}`]" />
            </div>
          </div>
        </fieldset>

        <fieldset class="grid gap-4 sm:grid-cols-2">
          <legend class="mb-2 text-lg font-semibold text-ink">Tipografia e formas</legend>
          <div class="space-y-1.5">
            <label :for="ids.body" class="block text-sm font-semibold text-ink">Fonte do texto</label>
            <select :id="ids.body" v-model="tokens.typography.body" class="block min-h-11 w-full rounded-control border border-muted bg-surface px-3 text-ink">
              <option v-for="font in theme.choices.fonts" :key="font" :value="font">{{ FONTS[font] ?? font }}</option>
            </select>
          </div>
          <div class="space-y-1.5">
            <label :for="ids.display" class="block text-sm font-semibold text-ink">Fonte dos títulos</label>
            <select :id="ids.display" v-model="tokens.typography.display" class="block min-h-11 w-full rounded-control border border-muted bg-surface px-3 text-ink">
              <option v-for="font in theme.choices.fonts" :key="font" :value="font">{{ FONTS[font] ?? font }}</option>
            </select>
          </div>
          <div class="space-y-1.5">
            <label :for="ids.radius" class="block text-sm font-semibold text-ink">Cantos</label>
            <select :id="ids.radius" v-model="tokens.shape.radius" class="block min-h-11 w-full rounded-control border border-muted bg-surface px-3 text-ink">
              <option v-for="value in theme.choices.radius" :key="value" :value="value">{{ RADIUS[value] ?? value }}</option>
            </select>
          </div>
          <div class="space-y-1.5">
            <label :for="ids.density" class="block text-sm font-semibold text-ink">Espaçamento</label>
            <select :id="ids.density" v-model="tokens.spacing.density" class="block min-h-11 w-full rounded-control border border-muted bg-surface px-3 text-ink">
              <option v-for="value in theme.choices.density" :key="value" :value="value">{{ DENSITY[value] ?? value }}</option>
            </select>
          </div>
          <div class="space-y-1.5">
            <label :for="ids.header" class="block text-sm font-semibold text-ink">Cabeçalho</label>
            <select :id="ids.header" v-model="tokens.layout.header" class="block min-h-11 w-full rounded-control border border-muted bg-surface px-3 text-ink">
              <option v-for="value in theme.choices.headers" :key="value" :value="value">{{ HEADERS[value] ?? value }}</option>
            </select>
          </div>
        </fieldset>

        <fieldset class="grid gap-4 sm:grid-cols-2">
          <legend class="mb-2 text-lg font-semibold text-ink">Marca</legend>
          <UiInput v-model="tokens.assets.logoUrl" label="Endereço do logotipo" :required="false" :error="errors['assets.logoUrl']" hint="HTTPS. Em branco, mostra o nome da loja." />
          <UiInput v-model="tokens.assets.faviconUrl" label="Endereço do ícone da aba" :required="false" :error="errors['assets.faviconUrl']" />
        </fieldset>

        <UiButton type="submit" :disabled="busy">{{ busy ? 'Publicando…' : 'Publicar aparência' }}</UiButton>
      </form>

      <div class="space-y-6">
        <section aria-labelledby="preview-title" class="space-y-2">
          <h2 id="preview-title" class="text-lg font-semibold text-ink">Prévia</h2>
          <div class="theme-preview space-y-3 border p-4" :style="preview">
            <div class="theme-card space-y-2 border p-4">
              <p class="theme-title">Produto em destaque</p>
              <p class="theme-muted text-sm">Texto secundário de exemplo</p>
              <span class="theme-button inline-block px-4 py-2 text-sm font-semibold">Adicionar ao carrinho</span>
            </div>
          </div>
        </section>
        <section aria-labelledby="versions-title" class="space-y-2">
          <h2 id="versions-title" class="text-lg font-semibold text-ink">Versões</h2>
          <ul class="divide-y divide-line rounded-card border border-line bg-surface px-4">
            <li v-for="version in theme.versions" :key="version.version" class="flex flex-wrap items-center justify-between gap-2 py-3">
              <span class="text-sm text-ink">Versão {{ version.version }} · {{ formatDate(version.createdAt) }}</span>
              <UiBadge v-if="version.status === 'published'" tone="success">Em uso</UiBadge>
              <UiButton v-else @click="rollback(version.version)">Usar<span class="sr-only"> a versão {{ version.version }}</span></UiButton>
            </li>
          </ul>
        </section>
      </div>
    </div>
  </div>
</template>

<style scoped>
.theme-preview {
  background: var(--p-canvas);
  border-color: var(--p-line);
  border-radius: var(--p-radius);
}
.theme-card {
  background: var(--p-surface);
  border-color: var(--p-line);
  border-radius: var(--p-radius);
}
.theme-title {
  color: var(--p-ink);
  font-weight: 600;
}
.theme-muted {
  color: var(--p-muted);
}
.theme-button {
  background: var(--p-primary);
  color: var(--p-on-primary);
  border-radius: var(--p-radius);
}
</style>
