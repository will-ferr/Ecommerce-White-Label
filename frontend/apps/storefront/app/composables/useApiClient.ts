import { createApiClient } from '@whitelabel/api-client'

/** On the server, requests use the internal network and identify the visited store. */
export function useApiClient() {
  const config = useRuntimeConfig()
  if (import.meta.server) {
    const internalFetch = useRequestEvent()?.context.internalFetch as typeof globalThis.fetch | undefined
    return createApiClient({ baseUrl: config.apiInternalUrl, fetch: internalFetch })
  }
  return createApiClient()
}
