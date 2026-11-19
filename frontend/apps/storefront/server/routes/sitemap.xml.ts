// Sitemap of the store being visited. Stores closed to search engines list nothing.
export default defineEventHandler(async (event) => {
  const { entries } = await loadSitemap(event)
  const origin = storeOrigin(event)
  const urls = entries.map((entry) => {
    const lastmod = entry.updatedAt ? `<lastmod>${escapeXml(entry.updatedAt.slice(0, 10))}</lastmod>` : ''
    return `<url><loc>${escapeXml(origin + entry.path)}</loc>${lastmod}</url>`
  })
  setHeader(event, 'content-type', 'application/xml; charset=utf-8')
  setHeader(event, 'cache-control', 'public, max-age=600')
  return `<?xml version="1.0" encoding="UTF-8"?>\n<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">${urls.join('')}</urlset>\n`
})
