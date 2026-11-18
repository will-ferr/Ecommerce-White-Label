import { describe, expect, it, vi } from 'vitest'

import { ApiError, NETWORK_ERROR, UNEXPECTED_RESPONSE, createApiClient } from './index'

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status })
}

function clientReturning(response: Response) {
  return createApiClient({ fetch: vi.fn().mockResolvedValue(response) })
}

describe('getStorefrontConfig', () => {
  it('returns the configuration of the store', async () => {
    const fetcher = vi.fn().mockResolvedValue(json({ slug: 'aurora' }))
    const client = createApiClient({ baseUrl: 'http://api:8080', fetch: fetcher })

    await expect(client.getStorefrontConfig()).resolves.toMatchObject({ slug: 'aurora' })
    expect(fetcher.mock.calls[0]?.[0]).toBe('http://api:8080/api/v1/storefront/config')
  })

  it('reports a store that does not exist', async () => {
    const client = clientReturning(json({ title: 'Not Found', status: 404, code: 'STORE_NOT_FOUND' }, 404))

    await expect(client.getStorefrontConfig()).rejects.toMatchObject({ status: 404, code: 'STORE_NOT_FOUND' })
  })
})

describe('getStatus', () => {
  it('returns the platform status', async () => {
    const fetcher = vi.fn().mockResolvedValue(json({ status: 'ok', version: '1.0.0' }))
    const client = createApiClient({ baseUrl: 'http://api:8080', fetch: fetcher })

    await expect(client.getStatus()).resolves.toEqual({ status: 'ok', version: '1.0.0' })
    expect(fetcher.mock.calls[0]?.[0]).toBe('http://api:8080/api/v1/status')
  })

  it('treats 503 as a valid outage report', async () => {
    const client = clientReturning(json({ status: 'unavailable', version: '1.0.0' }, 503))

    await expect(client.getStatus()).resolves.toMatchObject({ status: 'unavailable' })
  })

  it('converts problem responses into ApiError', async () => {
    const client = clientReturning(
      json(
        {
          type: 'urn:ecommerce:problem:RATE_LIMITED',
          title: 'Too Many Requests',
          status: 429,
          detail: 'Slow down.',
          code: 'RATE_LIMITED',
          correlation_id: 'corr-1',
          errors: [{ field: 'sku', code: 'required', message: 'Required.' }],
        },
        429,
      ),
    )

    const error = await client.getStatus().catch((e: unknown) => e)

    expect(error).toBeInstanceOf(ApiError)
    expect(error).toMatchObject({
      status: 429,
      code: 'RATE_LIMITED',
      message: 'Slow down.',
      correlationId: 'corr-1',
      fieldErrors: [{ field: 'sku' }],
    })
  })

  it('reports non-JSON gateway errors', async () => {
    const client = clientReturning(new Response('<html>Bad Gateway</html>', { status: 502 }))

    await expect(client.getStatus()).rejects.toMatchObject({ status: 502, code: UNEXPECTED_RESPONSE })
  })

  it('reports connection failures', async () => {
    const client = createApiClient({ fetch: vi.fn().mockRejectedValue(new TypeError('failed')) })

    await expect(client.getStatus()).rejects.toMatchObject({ status: 0, code: NETWORK_ERROR })
  })

  it('lets aborted requests propagate', async () => {
    const abort = new DOMException('Aborted', 'AbortError')
    const client = createApiClient({ fetch: vi.fn().mockRejectedValue(abort) })

    await expect(client.getStatus()).rejects.toBe(abort)
  })
})
