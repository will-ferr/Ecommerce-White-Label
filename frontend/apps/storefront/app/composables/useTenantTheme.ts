import { safeAssetUrl, themeToCss } from '@whitelabel/theme'

/** Applies the identity of the current store to the document. */
export function useTenantTheme() {
  const tenant = useTenantStore()

  useHead(() => {
    const config = tenant.config
    if (!config) return {}
    const favicon = safeAssetUrl(config.theme.tokens.assets.faviconUrl)
    return {
      htmlAttrs: { lang: config.store.locale },
      style: [{ key: 'tenant-theme', innerHTML: themeToCss(config.theme.tokens) }],
      link: favicon ? [{ key: 'icon', rel: 'icon', href: favicon }] : [],
    }
  })

  useSeoMeta({
    title: () => tenant.config?.seo.title || tenant.config?.store.name,
    description: () => tenant.config?.seo.description || undefined,
    ogTitle: () => tenant.config?.seo.title || tenant.config?.store.name,
    ogDescription: () => tenant.config?.seo.description || undefined,
    ogSiteName: () => tenant.config?.store.name,
    robots: () => (tenant.config?.seo.indexable ? 'index,follow' : 'noindex,nofollow'),
  })
}
