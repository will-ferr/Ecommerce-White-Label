<script setup lang="ts">
import type { ProductCard } from '@whitelabel/api-client'

defineProps<{ products: ProductCard[] }>()

const money = useMoney()
</script>

<template>
  <ul class="grid grid-cols-2 gap-4 sm:gap-6 lg:grid-cols-4">
    <li v-for="product in products" :key="product.id">
      <NuxtLink :to="`/p/${product.slug}`" class="group block space-y-3">
        <ProductImage :image="product.image" :name="product.name" />
        <div class="space-y-1">
          <p class="font-semibold text-ink group-hover:underline">{{ product.name }}</p>
          <p class="text-sm text-ink">
            <span v-if="product.hasPriceRange" class="text-muted">A partir de </span>
            <span class="font-semibold">{{ money(product.price) }}</span>
            <s v-if="product.compareAtPrice" class="ml-2 text-muted">{{ money(product.compareAtPrice) }}</s>
          </p>
          <p v-if="!product.available" class="text-sm font-semibold text-danger">Esgotado</p>
        </div>
      </NuxtLink>
    </li>
  </ul>
</template>
