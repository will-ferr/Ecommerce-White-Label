import { request as httpRequest } from 'node:http'
import { request as httpsRequest } from 'node:https'

const TIMEOUT_MS = 5000
const MAX_RESPONSE_BYTES = 1024 * 1024

/**
 * Calls the API on the internal network as the store being visited. The global
 * fetch replaces the Host header with the URL host, and Host identifies the store.
 */
function createInternalFetch(host: string): typeof globalThis.fetch {
  return (input, init) =>
    new Promise((resolve, reject) => {
      const url = new URL(String(input))
      const send = url.protocol === 'https:' ? httpsRequest : httpRequest
      const headers = { ...Object.fromEntries(new Headers(init?.headers)), host }

      const outgoing = send(url, { method: init?.method ?? 'GET', headers, signal: init?.signal ?? undefined }, (incoming) => {
        const chunks: Buffer[] = []
        let size = 0
        incoming.on('data', (chunk: Buffer) => {
          size += chunk.length
          if (size > MAX_RESPONSE_BYTES) {
            outgoing.destroy(new Error('Internal API response is too large'))
            return
          }
          chunks.push(chunk)
        })
        incoming.on('error', reject)
        incoming.on('end', () => {
          resolve(new Response(Buffer.concat(chunks), { status: incoming.statusCode }))
        })
      })

      outgoing.setTimeout(TIMEOUT_MS, () => outgoing.destroy(new Error('Internal API request timed out')))
      outgoing.on('error', reject)
      outgoing.end()
    })
}

export default defineEventHandler((event) => {
  event.context.internalFetch = createInternalFetch(getRequestHost(event))
})
