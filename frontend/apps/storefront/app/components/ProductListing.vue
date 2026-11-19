<script setup lang="ts">
import type { ProductCard } from '@whitelabel/api-client'
import { UiButton } from '@whitelabel/ui'

const props = defineProps<{ initial: ProductCard[]; nextCursor?: string; category?: string; search?: string }>()

const products = ref<ProductCard[]>([...props.initial])
const cursor = ref(props.nextCursor)
const loading = ref(false)
const failed = ref(false)

watch(
  () => props.initial,
  (value) => {
    products.value = [...value]
    cursor.value = props.nextCursor
  },
)

async function loadMore() {
  loading.value = true
  failed.value = false
  try {
    const page = await useStoreApi().products({ category: props.category, q: props.search, cursor: cursor.value })
    products.value.push(...page.items)
    cursor.value = page.nextCursor
  } catch {
    failed.value = true
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="space-y-8">
    <p v-if="products.length === 0" class="rounded-card border border-line bg-surface p-6 text-muted">
      Nenhum produto encontrado.
    </p>
    <ProductGrid v-else :products="products" />
    <p v-if="failed" role="alert" class="text-sm text-danger">Não foi possível carregar mais produtos. Tente novamente.</p>
    <UiButton v-if="cursor" :disabled="loading" @click="loadMore">{{ loading ? 'Carregando…' : 'Ver mais produtos' }}</UiButton>
  </div>
</template>
