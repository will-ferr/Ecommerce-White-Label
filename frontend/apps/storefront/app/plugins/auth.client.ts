export default defineNuxtPlugin({
  name: 'store-auth',
  dependsOn: ['pinia'],
  setup() {
    const cart = useCartStore()
    // The cart depends on the session, so it loads after the session is restored.
    void useStoreAuth()
      .restore()
      .then(() => cart.load())
      .catch(() => undefined)
  },
})
