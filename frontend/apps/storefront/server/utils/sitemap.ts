import type { H3Event } from 'h3'

export interface SitemapData {
  indexable: boolean
  entries: { path: string; updatedAt?: string }[]
}

/** Reads the public addresses of the store being visited from the API. */
export async function loadSitemap(event: H3Event): Promise<SitemapData> {
  const config = useRuntimeConfig(event)
  const internalFetch = event.context.internalFetch as typeof globalThis.fetch
  const response = await internalFetch(`${config.apiInternalUrl}/api/v1/storefront/sitemap`, { headers: { accept: 'application/json' } })
  if (response.status === 404) throw createError({ statusCode: 404, statusMessage: 'STORE_NOT_FOUND' })
  if (!response.ok) throw createError({ statusCode: 503, statusMessage: 'UNAVAILABLE' })
  return (await response.json()) as SitemapData
}

/** The public origin of the store, as the visitor reached it. */
export function storeOrigin(event: H3Event): string {
  return getRequestURL(event, { xForwardedProto: true }).origin
}

export function escapeXml(text: string): string {
  return text.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;').replace(/'/g, '&apos;')
}
