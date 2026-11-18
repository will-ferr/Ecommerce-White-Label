import { createApiClient } from '@whitelabel/api-client'

/** On the server the API is reached through the internal network. */
export function useApiClient() {
  const config = useRuntimeConfig()
  return createApiClient({ baseUrl: import.meta.server ? config.apiInternalUrl : '' })
}
