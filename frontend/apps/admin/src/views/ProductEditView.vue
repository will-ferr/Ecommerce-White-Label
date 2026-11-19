<script setup lang="ts">
import { ApiError, type AdminProduct, type AdminVariant, type Category, type Image } from '@whitelabel/api-client'
import { errorMessage } from '@whitelabel/auth'
import { useAuth } from '@whitelabel/panel-shell'
import { centsToInput, parseMoney, UiAlert, UiButton, UiInput, UiSkeleton } from '@whitelabel/ui'
import { computed, onMounted, reactive, ref, useId, watch } from 'vue'
import { useRouter } from 'vue-router'

import { PRODUCT_STATUS_LABELS, useCommerce } from '../commerce'

const props = defineProps<{ id?: string }>()

interface VariantRow {
  id?: string
  values: string[]
  sku: string
  gtin: string
  price: string
  compareAt: string
  cost: string
  weight: string
  stock?: AdminVariant['stock']
  delta: string
  reason: string
}

const api = useCommerce()
const auth = useAuth()
const router = useRouter()
const canEdit = computed(() => auth.can('catalog:manage'))
const canAdjust = computed(() => auth.can('inventory:manage'))
const fieldId = useId()

const loaded = ref(!props.id)
const categories = ref<Category[]>([])
const version = ref(0)
const form = reactive({
  name: '', slug: '', description: '', brand: '', status: 'draft' as AdminProduct['status'], categoryId: '' as string,
  featured: false, seoTitle: '', seoDescription: '',
})
const images = ref<Image[]>([])
const optionNames = ref<string[]>([''])
const variants = ref<VariantRow[]>([blankVariant()])
const failure = ref<string>()
const notice = ref<string>()
const busy = ref(false)

function blankVariant(): VariantRow {
  return { values: optionNames.value?.map(() => '') ?? [''], sku: '', gtin: '', price: '', compareAt: '', cost: '', weight: '0', delta: '', reason: '' }
}

function fill(product: AdminProduct) {
  version.value = product.version
  Object.assign(form, {
    name: product.name, slug: product.slug, description: product.description, brand: product.brand, status: product.status,
    categoryId: product.categoryId ?? '', featured: product.featured, seoTitle: product.seoTitle, seoDescription: product.seoDescription,
  })
  images.value = product.images.map((image) => ({ ...image }))
  const active = product.variants.filter((variant) => variant.status !== 'archived')
  optionNames.value = Object.keys(active[0]?.options ?? {}).sort()
  if (!optionNames.value.length) optionNames.value = ['']
  variants.value = active.map((variant) => ({
    id: variant.id, values: optionNames.value.map((name) => variant.options[name] ?? ''), sku: variant.sku, gtin: variant.gtin,
    price: centsToInput(variant.price), compareAt: centsToInput(variant.compareAtPrice), cost: centsToInput(variant.costPrice),
    weight: String(variant.weightGrams), stock: variant.stock, delta: '', reason: '',
  }))
}

onMounted(async () => {
  try {
    categories.value = await api.categories()
    if (props.id) fill(await api.product(props.id))
  } catch (error) {
    failure.value = errorMessage(error)
  } finally {
    loaded.value = true
  }
})

// After a new product is created the route changes to its address and this
// view is reused, so the saved product and its stock must be loaded here.
watch(() => props.id, async (id) => {
  if (!id) return
  try {
    fill(await api.product(id))
  } catch (error) {
    failure.value = errorMessage(error)
  }
})

function addOption() {
  if (optionNames.value.length >= 3) return
  optionNames.value.push('')
  variants.value.forEach((variant) => variant.values.push(''))
}

function removeOption(index: number) {
  optionNames.value.splice(index, 1)
  variants.value.forEach((variant) => variant.values.splice(index, 1))
  if (!optionNames.value.length) addOption()
}

function buildInput() {
  const names = optionNames.value.map((name) => name.trim())
  const variantInputs = variants.value.map((row) => {
    const price = parseMoney(row.price)
    if (price === null) throw new Error(`Informe um preço válido para o SKU ${row.sku || '(sem SKU)'}.`)
    const options: Record<string, string> = {}
    names.forEach((name, index) => {
      if (name) options[name] = row.values[index]?.trim() ?? ''
    })
    return {
      id: row.id, sku: row.sku.trim(), gtin: row.gtin.trim(), options, weightGrams: Number(row.weight) || 0, price,
      compareAtPrice: parseMoney(row.compareAt), costPrice: parseMoney(row.cost),
    }
  })
  return {
    ...form, categoryId: form.categoryId || null, version: version.value,
    images: images.value.filter((image) => image.url.trim()), variants: variantInputs,
  }
}

async function save() {
  busy.value = true
  failure.value = undefined
  notice.value = undefined
  try {
    const input = buildInput()
    if (props.id) {
      await api.updateProduct(props.id, input)
      fill(await api.product(props.id))
      notice.value = 'Produto salvo.'
    } else {
      const created = await api.createProduct(input)
      await router.replace(`/produtos/${created.id}`)
    }
  } catch (error) {
    failure.value = error instanceof ApiError ? describe(error) : error instanceof Error ? error.message : errorMessage(error)
  } finally {
    busy.value = false
  }
}

function describe(error: ApiError) {
  const messages: Record<string, string> = {
    VERSION_CONFLICT: 'Outra pessoa alterou este produto. Recarregue a página antes de salvar.',
    SKU_IN_USE: 'Outro produto já usa um destes SKUs.',
    SLUG_IN_USE: 'Outro produto já usa este endereço.',
  }
  if (messages[error.code]) return messages[error.code]!
  const field = error.fieldErrors[0]
  return field ? `Confira o campo ${field.field}: ${field.message}` : errorMessage(error)
}

async function adjust(row: VariantRow) {
  if (!row.id) return
  failure.value = undefined
  notice.value = undefined
  const delta = Number(row.delta)
  if (!Number.isInteger(delta) || delta === 0) {
    failure.value = 'Informe uma quantidade inteira, positiva para entrada ou negativa para saída.'
    return
  }
  try {
    row.stock = await api.adjustStock({ variantId: row.id, delta, reason: row.reason || 'Ajuste manual' })
    row.delta = ''
    row.reason = ''
    notice.value = 'Estoque atualizado.'
  } catch (error) {
    failure.value = error instanceof ApiError && error.code === 'STOCK_BELOW_RESERVED'
      ? 'O estoque não pode ficar abaixo do que está reservado por pedidos em aberto.'
      : errorMessage(error)
  }
}
</script>

<template>
  <div class="space-y-6">
    <h1 class="font-display text-3xl text-ink sm:text-4xl">{{ id ? form.name || 'Produto' : 'Novo produto' }}</h1>
    <UiAlert v-if="failure" :title="failure" />
    <p v-if="notice" role="status" class="rounded-control bg-success-soft px-4 py-3 text-sm font-semibold text-success">{{ notice }}</p>
    <UiSkeleton v-if="!loaded" class="h-64 w-full" />

    <form v-else class="space-y-6" @submit.prevent="save">
      <fieldset class="grid gap-4 rounded-card border border-line bg-surface p-5 sm:grid-cols-2 sm:p-6" :disabled="!canEdit">
        <legend class="px-1 font-display text-2xl text-ink">Informações</legend>
        <UiInput v-model="form.name" label="Nome" />
        <UiInput v-model="form.slug" label="Endereço (opcional)" :required="false" hint="Gerado a partir do nome quando vazio." />
        <UiInput v-model="form.brand" label="Marca" :required="false" />
        <div class="space-y-1.5">
          <label :for="`${fieldId}-category`" class="block text-sm font-semibold text-ink">Categoria</label>
          <select :id="`${fieldId}-category`" v-model="form.categoryId" class="block min-h-11 w-full rounded-control border border-muted bg-surface px-3 text-ink">
            <option value="">Sem categoria</option>
            <option v-for="category in categories" :key="category.id" :value="category.id">{{ category.name }}</option>
          </select>
        </div>
        <div class="space-y-1.5">
          <label :for="`${fieldId}-status`" class="block text-sm font-semibold text-ink">Situação</label>
          <select :id="`${fieldId}-status`" v-model="form.status" class="block min-h-11 w-full rounded-control border border-muted bg-surface px-3 text-ink">
            <option v-for="(label, value) in PRODUCT_STATUS_LABELS" :key="value" :value="value">{{ label }}</option>
          </select>
        </div>
        <label class="flex min-h-11 items-center gap-3 text-ink">
          <input v-model="form.featured" type="checkbox" class="size-5 accent-[var(--wl-color-primary)]" />
          Mostrar nos destaques
        </label>
        <div class="space-y-1.5 sm:col-span-2">
          <label :for="`${fieldId}-description`" class="block text-sm font-semibold text-ink">Descrição</label>
          <textarea :id="`${fieldId}-description`" v-model="form.description" rows="5" maxlength="5000" class="block w-full rounded-control border border-muted bg-surface p-3 text-ink"></textarea>
        </div>
        <UiInput v-model="form.seoTitle" label="Título para buscadores" :required="false" hint="Até 70 caracteres." />
        <UiInput v-model="form.seoDescription" label="Descrição para buscadores" :required="false" hint="Até 160 caracteres." />
      </fieldset>

      <fieldset class="space-y-4 rounded-card border border-line bg-surface p-5 sm:p-6" :disabled="!canEdit">
        <legend class="px-1 font-display text-2xl text-ink">Imagens</legend>
        <p class="text-sm text-muted">Informe endereços HTTPS de imagens já publicadas.</p>
        <div v-for="(image, index) in images" :key="index" class="grid gap-3 sm:grid-cols-[1fr_1fr_auto] sm:items-end">
          <UiInput v-model="image.url" :label="`Endereço da imagem ${index + 1}`" type="url" />
          <UiInput v-model="image.alt" label="Descrição da imagem" :required="false" />
          <UiButton @click="images.splice(index, 1)">Remover</UiButton>
        </div>
        <UiButton v-if="images.length < 10" @click="images.push({ url: '', alt: '' })">Adicionar imagem</UiButton>
      </fieldset>

      <fieldset class="space-y-4 rounded-card border border-line bg-surface p-5 sm:p-6" :disabled="!canEdit">
        <legend class="px-1 font-display text-2xl text-ink">Variações</legend>
        <div class="flex flex-wrap items-end gap-3">
          <div v-for="(_, index) in optionNames" :key="index" class="flex items-end gap-2">
            <UiInput v-model="optionNames[index]" :label="`Opção ${index + 1}`" :required="false" hint="Ex.: Cor, Tamanho" />
            <UiButton v-if="optionNames.length > 1" @click="removeOption(index)">Remover</UiButton>
          </div>
          <UiButton v-if="optionNames.length < 3" @click="addOption">Adicionar opção</UiButton>
        </div>

        <div v-for="(row, index) in variants" :key="row.id ?? index" class="space-y-3 rounded-control border border-line p-4">
          <div class="grid gap-3 sm:grid-cols-3">
            <template v-for="(name, optionIndex) in optionNames" :key="optionIndex">
              <UiInput v-if="name.trim()" v-model="row.values[optionIndex]" :label="name" />
            </template>
            <UiInput v-model="row.sku" label="SKU" />
            <UiInput v-model="row.price" label="Preço (R$)" inputmode="numeric" />
            <UiInput v-model="row.compareAt" label="Preço original (R$)" inputmode="numeric" :required="false" />
            <UiInput v-model="row.cost" label="Custo (R$)" inputmode="numeric" :required="false" />
            <UiInput v-model="row.weight" label="Peso (g)" inputmode="numeric" :required="false" />
            <UiInput v-model="row.gtin" label="GTIN/EAN" inputmode="numeric" :required="false" />
          </div>
          <UiButton v-if="variants.length > 1" @click="variants.splice(index, 1)">Remover variação</UiButton>
        </div>
        <UiButton @click="variants.push(blankVariant())">Adicionar variação</UiButton>
      </fieldset>

      <UiButton v-if="canEdit" type="submit" :disabled="busy">{{ busy ? 'Salvando…' : 'Salvar produto' }}</UiButton>
    </form>

    <section v-if="id && loaded" class="space-y-4 rounded-card border border-line bg-surface p-5 sm:p-6" aria-labelledby="stock-title">
      <h2 id="stock-title" class="font-display text-2xl text-ink">Estoque</h2>
      <div v-for="row in variants.filter((item) => item.id)" :key="row.id" class="flex flex-wrap items-end justify-between gap-3 border-t border-line pt-4 first-of-type:border-0 first-of-type:pt-0">
        <div>
          <p class="font-semibold text-ink">{{ row.values.filter(Boolean).join(' / ') || row.sku }}</p>
          <p class="text-sm text-muted" :data-testid="`stock-${row.sku}`">
            {{ row.stock?.onHand ?? 0 }} em estoque · {{ row.stock?.reserved ?? 0 }} reservados · {{ row.stock?.available ?? 0 }} disponíveis
          </p>
        </div>
        <form v-if="canAdjust" class="flex flex-wrap items-end gap-2" @submit.prevent="adjust(row)">
          <UiInput v-model="row.delta" :label="`Ajuste de ${row.sku}`" inputmode="numeric" hint="Use valores negativos para saídas." />
          <UiInput v-model="row.reason" label="Motivo" :required="false" />
          <UiButton type="submit">Aplicar</UiButton>
        </form>
      </div>
    </section>
  </div>
</template>
