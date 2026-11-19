<script setup lang="ts">
import { errorMessage } from '@whitelabel/auth'
import { UiAlert, UiButton, UiSkeleton } from '@whitelabel/ui'

const cart = useCartStore()
const money = useMoney()
const failure = ref<string>()

useSeoMeta({ title: 'Carrinho', robots: 'noindex,nofollow' })

onMounted(async () => {
  try {
    await cart.load()
  } catch (error) {
    failure.value = errorMessage(error)
  }
})

async function change(action: () => Promise<void>) {
  failure.value = undefined
  try {
    await action()
  } catch (error) {
    failure.value = errorMessage(error)
  }
}
</script>

<template>
  <StoreLayout>
    <div class="space-y-6">
      <h1 class="font-display text-4xl text-ink">Carrinho</h1>
      <UiAlert v-if="failure" :title="failure" />
      <UiSkeleton v-if="!cart.cart" class="h-40 w-full" />

      <div v-else-if="cart.cart.lines.length === 0" class="space-y-4 rounded-card border border-line bg-surface p-6">
        <p class="text-ink">Seu carrinho está vazio.</p>
        <NuxtLink to="/" class="font-semibold text-primary underline underline-offset-4">Ver produtos</NuxtLink>
      </div>

      <div v-else class="grid gap-6 lg:grid-cols-[1fr_20rem]">
        <ul class="divide-y divide-line rounded-card border border-line bg-surface px-5">
          <li v-for="line in cart.cart.lines" :key="line.variantId" class="flex flex-wrap items-center justify-between gap-4 py-5">
            <div class="min-w-0">
              <NuxtLink v-if="line.productSlug" :to="`/p/${line.productSlug}`" class="font-semibold text-ink underline-offset-4 hover:underline">{{ line.productName }}</NuxtLink>
              <p v-else class="font-semibold text-ink">{{ line.productName }}</p>
              <p v-if="line.label" class="text-sm text-muted">{{ line.label }}</p>
              <p v-if="!line.available" class="text-sm font-semibold text-danger">Indisponível na quantidade escolhida</p>
            </div>
            <div class="flex items-center gap-3">
              <label :for="`qty-${line.variantId}`" class="sr-only">Quantidade de {{ line.productName }}</label>
              <select
                :id="`qty-${line.variantId}`"
                :value="line.quantity"
                class="min-h-11 rounded-control border border-muted bg-surface px-3 text-ink"
                @change="change(() => cart.set(line.variantId, Number(($event.target as HTMLSelectElement).value)))"
              >
                <option v-for="n in Math.max(10, line.quantity)" :key="n" :value="n">{{ n }}</option>
              </select>
              <p class="w-28 text-right font-semibold text-ink">{{ money(line.total) }}</p>
              <button type="button" class="min-h-11 cursor-pointer text-sm font-semibold text-primary underline underline-offset-4" @click="change(() => cart.remove(line.variantId))">
                Remover<span class="sr-only"> {{ line.productName }}</span>
              </button>
            </div>
          </li>
        </ul>

        <aside class="h-fit space-y-4 rounded-card border border-line bg-surface p-5" aria-label="Resumo">
          <p class="flex justify-between text-ink"><span>Subtotal</span><span class="font-semibold">{{ money(cart.cart.subtotal) }}</span></p>
          <p class="text-sm text-muted">O frete é calculado na próxima etapa.</p>
          <UiAlert v-if="cart.cart.hasUnavailable" title="Remova os itens indisponíveis para continuar." />
          <NuxtLink v-else to="/checkout" class="block">
            <UiButton class="w-full">Finalizar compra</UiButton>
          </NuxtLink>
        </aside>
      </div>
    </div>
  </StoreLayout>
</template>
