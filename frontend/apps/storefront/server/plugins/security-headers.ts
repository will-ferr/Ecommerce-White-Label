import { randomBytes } from 'node:crypto'

// Content Security Policy of the storefront. Pages are rendered on the server
// with inline scripts (the Nuxt payload), so every HTML answer gets a new
// nonce and only scripts carrying it, or loaded from the store itself, run.
// Styles stay inline because each store paints its theme with CSS variables.
function policy(nonce: string) {
  return [
    "default-src 'self'",
    `script-src 'self' 'nonce-${nonce}'`,
    "style-src 'self' 'unsafe-inline'",
    "img-src 'self' data: https:",
    "font-src 'self' data:",
    "connect-src 'self'",
    "object-src 'none'",
    "base-uri 'none'",
    "form-action 'self'",
    "frame-ancestors 'none'",
  ].join('; ')
}

const withNonce = (parts: string[], nonce: string) =>
  parts.map((part) => part.replace(/<script(?![^>]*\bnonce=)/g, `<script nonce="${nonce}"`))

export default defineNitroPlugin((nitro) => {
  nitro.hooks.hook('render:html', (html, { event }) => {
    const nonce = randomBytes(16).toString('base64')
    html.head = withNonce(html.head, nonce)
    html.bodyPrepend = withNonce(html.bodyPrepend, nonce)
    html.body = withNonce(html.body, nonce)
    html.bodyAppend = withNonce(html.bodyAppend, nonce)
    setResponseHeader(event, 'Content-Security-Policy', policy(nonce))
  })

  // Applies to every answer, static files included.
  nitro.hooks.hook('beforeResponse', (event) => {
    event.node.res.removeHeader('x-powered-by')
    setResponseHeaders(event, {
      'X-Content-Type-Options': 'nosniff',
      'X-Frame-Options': 'DENY',
      'Referrer-Policy': 'strict-origin-when-cross-origin',
      'Permissions-Policy': 'camera=(), microphone=(), geolocation=(), payment=(), usb=()',
      'Cross-Origin-Opener-Policy': 'same-origin',
      'Cross-Origin-Resource-Policy': 'same-origin',
    })
  })
})
