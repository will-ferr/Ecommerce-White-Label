// Crawling rules of the store being visited. Private pages are never indexed,
// and a store that is not open to search engines blocks everything.
export default defineEventHandler(async (event) => {
  const { indexable } = await loadSitemap(event)
  setHeader(event, 'content-type', 'text/plain; charset=utf-8')
  setHeader(event, 'cache-control', 'public, max-age=600')
  if (!indexable) return 'User-agent: *\nDisallow: /\n'
  return [
    'User-agent: *',
    'Disallow: /carrinho',
    'Disallow: /checkout',
    'Disallow: /conta',
    'Disallow: /entrar',
    'Disallow: /pedido/',
    'Disallow: /busca',
    `Sitemap: ${storeOrigin(event)}/sitemap.xml`,
    '',
  ].join('\n')
})
