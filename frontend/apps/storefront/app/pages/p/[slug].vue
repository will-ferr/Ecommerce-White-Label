<script setup lang="ts">
import { ApiError, type StoreVariant } from '@whitelabel/api-client'
import { errorMessage } from '@whitelabel/auth'
import { UiAlert, UiButton } from '@whitelabel/ui'

const route = useRoute()
const api = useStoreApi()
const tenant = useTenantStore()
const cart = useCartStore()
const money = useMoney()

const { data: product, error } = await useAsyncData(`product-${route.params.slug}`, () => api.product(String(route.params.slug)))
if (error.value || !product.value) {
  const status = error.value instanceof ApiError ? error.value.status : 404
  throw createError({ statusCode: status === 404 ? 404 : 503, statusMessage: status === 404 ? 'PAGE_NOT_FOUND' : 'UNAVAILABLE', fatal: true })
}

const optionNames = computed(() => Object.keys(product.value?.variants[0]?.options ?? {}).sort())
const selection = reactive<Record<string, string>>({ ...(product.value.variants.find((v) => v.available) ?? product.value.variants[0])?.options })
const valuesOf = (name: string) => [...new Set(product.value?.variants.map((variant) => variant.options[name]).filter(Boolean))] as string[]

const variant = computed<StoreVariant | undefined>(() =>
  product.value?.variants.find((candidate) => optionNames.value.every((name) => candidate.options[name] === selection[name])),
)
const image = ref(0)
const quantity = ref(1)
const added = ref(false)
const failure = ref<string>()
const busy = ref(false)

async function addToCart() {
  if (!variant.value) return
  busy.value = true
  failure.value = undefined
  added.value = false
  try {
    await cart.add(variant.value.id, quantity.value)
    added.value = true
  } catch (cause) {
    failure.value = errorMessage(cause)
  } finally {
    busy.value = false
  }
}

const decimal = (cents: number) => (cents / 100).toFixed(2)

// Structured data lets search engines show price and availability.
const structuredData = computed(() => {
  const current = product.value
  if (!current) return '{}'
  const json = JSON.stringify({
    '@context': 'https://schema.org',
    '@type': 'Product',
    name: current.name,
    description: current.description || undefined,
    brand: current.brand ? { '@type': 'Brand', name: current.brand } : undefined,
    offers: current.variants.map((item) => ({
      '@type': 'Offer',
      sku: item.id,
      price: decimal(item.price),
      priceCurrency: tenant.config?.store.currency,
      availability: item.available ? 'https://schema.org/InStock' : 'https://schema.org/OutOfStock',
    })),
  })
  // Prevents a product text from closing the script element.
  return json.replace(/</g, '\\u003c')
})

useHead({ script: [{ key: 'product-ld', type: 'application/ld+json', innerHTML: structuredData }] })
useSeoMeta({
  title: () => product.value?.seoTitle || product.value?.name,
  description: () => product.value?.seoDescription || product.value?.description.slice(0, 160) || undefined,
  ogType: 'website',
  ogImage: () => product.value?.images[0]?.url,
})
</script>

<template>
  <StoreLayout>
    <article v-if="product" class="grid gap-8 lg:grid-cols-2 lg:gap-12">
      <div class="space-y-3">
        <ProductImage :image="product.images[image]" :name="product.name" />
        <ul v-if="product.images.length > 1" class="flex gap-2" aria-label="Fotos do produto">
          <li v-for="(picture, index) in product.images" :key="picture.url" class="w-20">
            <button type="button" class="block w-full cursor-pointer" :aria-pressed="index === image" :aria-label="`Ver foto ${index + 1}`" @click="image = index">
              <ProductImage :image="picture" :name="product.name" />
            </button>
          </li>
        </ul>
      </div>

      <div class="space-y-6">
        <nav v-if="product.category" aria-label="Categoria">
          <NuxtLink :to="`/c/${product.category.slug}`" class="text-sm font-semibold text-primary underline underline-offset-4">{{ product.category.name }}</NuxtLink>
        </nav>
        <div class="space-y-2">
          <p v-if="product.brand" class="text-sm text-muted">{{ product.brand }}</p>
          <h1 class="font-display text-3xl text-ink sm:text-4xl">{{ product.name }}</h1>
        </div>

        <p v-if="variant" class="text-2xl text-ink">
          <span class="font-semibold">{{ money(variant.price) }}</span>
          <s v-if="variant.compareAtPrice" class="ml-3 text-lg text-muted">{{ money(variant.compareAtPrice) }}</s>
        </p>

        <fieldset v-for="name in optionNames" :key="name" class="space-y-2">
          <legend class="text-sm font-semibold text-ink">{{ name }}</legend>
          <div class="flex flex-wrap gap-2">
            <button
              v-for="value in valuesOf(name)"
              :key="value"
              type="button"
              class="min-h-11 cursor-pointer rounded-control border px-4 text-sm font-semibold"
              :class="selection[name] === value ? 'border-primary bg-primary text-on-primary' : 'border-muted bg-surface text-ink'"
              :aria-pressed="selection[name] === value"
              @click="selection[name] = value"
            >
              {{ value }}
            </button>
          </div>
        </fieldset>

        <div v-if="variant?.available" class="flex flex-wrap items-end gap-3">
          <div class="space-y-1.5">
            <label for="quantity" class="block text-sm font-semibold text-ink">Quantidade</label>
            <input id="quantity" v-model.number="quantity" type="number" min="1" max="99" class="block min-h-11 w-24 rounded-control border border-muted bg-surface px-3 text-ink" />
          </div>
          <UiButton :disabled="busy" @click="addToCart">{{ busy ? 'Adicionando…' : 'Adicionar ao carrinho' }}</UiButton>
        </div>
        <p v-else class="font-semibold text-danger">{{ variant ? 'Esgotado' : 'Combinação indisponível' }}</p>

        <p v-if="added" role="status" class="rounded-control bg-success-soft px-4 py-3 text-sm font-semibold text-success">
          Produto adicionado. <NuxtLink to="/carrinho" class="underline underline-offset-4">Ver carrinho</NuxtLink>
        </p>
        <UiAlert v-if="failure" :title="failure" />

        <div v-if="product.description" class="space-y-2">
          <h2 class="font-display text-xl text-ink">Descrição</h2>
          <p class="whitespace-pre-line leading-relaxed text-ink">{{ product.description }}</p>
        </div>
      </div>
    </article>
  </StoreLayout>
</template>
