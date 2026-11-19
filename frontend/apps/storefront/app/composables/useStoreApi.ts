import { createPublicSender, createStoreClient, type StoreClient } from '@whitelabel/api-client'

/**
 * Store API for the current context. On the server it reads the public catalog
 * through the internal network; in the browser it sends the customer session
 * and the cart cookie.
 */
export function useStoreApi(): StoreClient {
  if (import.meta.server) {
    const config = useRuntimeConfig()
    const internalFetch = useRequestEvent()?.context.internalFetch as typeof globalThis.fetch | undefined
    return createStoreClient(createPublicSender(`${config.apiInternalUrl}/api/v1`, internalFetch))
  }
  const portal = useStoreAuth().client()
  return createStoreClient((method, path, body, headers) => portal.call(method, `/api/v1${path}`, body, headers))
}

/** Formats amounts in the currency and language of the store. */
export function useMoney() {
  const tenant = useTenantStore()
  return (cents: number) => {
    const currency = tenant.config?.store.currency ?? 'BRL'
    const locale = tenant.config?.store.locale ?? 'pt-BR'
    return new Intl.NumberFormat(locale, { style: 'currency', currency }).format(cents / 100)
  }
}
