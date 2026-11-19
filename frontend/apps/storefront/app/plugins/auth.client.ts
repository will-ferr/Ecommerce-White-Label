export default defineNuxtPlugin({
  name: 'store-auth',
  dependsOn: ['pinia'],
  setup() {
    void useStoreAuth().restore()
  },
})
