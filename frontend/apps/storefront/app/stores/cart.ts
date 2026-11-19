import type { Cart } from '@whitelabel/api-client'

export const useCartStore = defineStore('cart', () => {
  const cart = ref<Cart | null>(null)
  const count = computed(() => cart.value?.lines.reduce((total, line) => total + line.quantity, 0) ?? 0)

  async function load() {
    cart.value = await useStoreApi().cart()
  }

  async function add(variantId: string, quantity: number) {
    cart.value = await useStoreApi().addItem(variantId, quantity)
  }

  async function set(variantId: string, quantity: number) {
    cart.value = await useStoreApi().setItem(variantId, quantity)
  }

  async function remove(variantId: string) {
    cart.value = await useStoreApi().removeItem(variantId)
  }

  /** Moves the visitor cart into the account that has just signed in. */
  async function merge() {
    cart.value = await useStoreApi().mergeCart()
  }

  return { cart, count, load, add, set, remove, merge }
})
