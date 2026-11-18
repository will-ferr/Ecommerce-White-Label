import { ApiError, type ApiClient, type StorefrontConfig } from '@whitelabel/api-client'

interface TenantFailure {
  status: number
  code: string
}

export const useTenantStore = defineStore('tenant', () => {
  const config = ref<StorefrontConfig | null>(null)
  const failure = ref<TenantFailure | null>(null)

  async function load(client: ApiClient) {
    try {
      config.value = await client.getStorefrontConfig()
      failure.value = null
    } catch (error) {
      if (!(error instanceof ApiError)) throw error
      config.value = null
      failure.value = { status: error.status, code: error.code }
    }
  }

  return { config, failure, load }
})
