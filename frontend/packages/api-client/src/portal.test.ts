import { describe, expect, it, vi } from 'vitest'

import { ApiError } from './index'
import { createPortalClient } from './portal'

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status })
}

function problem(code: string, status: number) {
  return json({ type: 'urn:x', title: 'Error', status, code }, status)
}

const authenticated = (token: string) => json({ status: 'authenticated', accessToken: token })

function authorization(fetcher: ReturnType<typeof vi.fn>, call: number) {
  return (fetcher.mock.calls[call]?.[1] as RequestInit).headers as Record<string, string>
}

describe('createPortalClient', () => {
  it('sends the access token received at login', async () => {
    const fetcher = vi.fn().mockResolvedValueOnce(authenticated('token-1')).mockResolvedValueOnce(json({ id: '1' }))
    const client = createPortalClient('/api/v1/auth', fetcher)

    await client.login({ email: 'a@b.com', password: 'x' })
    await client.me()

    expect(fetcher.mock.calls[1]?.[0]).toBe('/api/v1/auth/me')
    expect(authorization(fetcher, 1).Authorization).toBe('Bearer token-1')
  })

  it('does not store a token while the second step is pending', async () => {
    const fetcher = vi.fn().mockResolvedValueOnce(json({ status: 'mfa_required', challenge: 'c' })).mockResolvedValueOnce(problem('AUTHENTICATION_REQUIRED', 401)).mockResolvedValueOnce(problem('SESSION_EXPIRED', 401))
    const client = createPortalClient('/api/v1/auth', fetcher)

    await client.login({ email: 'a@b.com', password: 'x' })

    await expect(client.me()).rejects.toBeInstanceOf(ApiError)
    expect(authorization(fetcher, 1).Authorization).toBeUndefined()
  })

  it('renews an expired token once and repeats the request', async () => {
    const fetcher = vi.fn()
      .mockResolvedValueOnce(authenticated('old'))
      .mockResolvedValueOnce(problem('SESSION_EXPIRED', 401))
      .mockResolvedValueOnce(authenticated('new'))
      .mockResolvedValueOnce(json({ id: '1' }))
    const client = createPortalClient('/api/v1/auth', fetcher)
    await client.login({ email: 'a@b.com', password: 'x' })

    await expect(client.me()).resolves.toEqual({ id: '1' })

    expect(fetcher.mock.calls[2]?.[0]).toBe('/api/v1/auth/refresh')
    expect(authorization(fetcher, 3).Authorization).toBe('Bearer new')
  })

  it('shares one renewal between simultaneous requests', async () => {
    let refreshes = 0
    const fetcher = vi.fn(async (url: string, init: RequestInit) => {
      if (url.endsWith('/login')) return authenticated('old')
      if (url.endsWith('/refresh')) {
        refreshes++
        return authenticated('new')
      }
      const headers = init.headers as Record<string, string>
      return headers.Authorization === 'Bearer new' ? json({ items: [] }) : problem('SESSION_EXPIRED', 401)
    })
    const client = createPortalClient('/api/v1/auth', fetcher as unknown as typeof fetch)
    await client.login({ email: 'a@b.com', password: 'x' })

    await Promise.all([client.listSessions(), client.accessHistory(), client.listSessions()])

    expect(refreshes).toBe(1)
  })

  it('retries when another tab has just renewed the session', async () => {
    vi.useFakeTimers()
    const fetcher = vi.fn().mockResolvedValueOnce(problem('REFRESH_IN_PROGRESS', 409)).mockResolvedValueOnce(authenticated('new'))
    const client = createPortalClient('/api/v1/auth', fetcher)

    const result = client.refresh()
    await vi.runAllTimersAsync()

    await expect(result).resolves.toMatchObject({ accessToken: 'new' })
    vi.useRealTimers()
  })

  it('reports the failure when the session cannot be renewed', async () => {
    const fetcher = vi.fn()
      .mockResolvedValueOnce(authenticated('old'))
      .mockResolvedValueOnce(problem('SESSION_EXPIRED', 401))
      .mockResolvedValueOnce(problem('SESSION_EXPIRED', 401))
    const client = createPortalClient('/api/v1/auth', fetcher)
    await client.login({ email: 'a@b.com', password: 'x' })

    await expect(client.me()).rejects.toMatchObject({ code: 'SESSION_EXPIRED' })
  })

  it('exposes field errors', async () => {
    const fetcher = vi.fn().mockResolvedValue(
      json({ title: 'Bad', status: 400, code: 'VALIDATION_FAILED', errors: [{ field: 'password', code: 'too_short', message: 'x' }] }, 400),
    )
    const client = createPortalClient('/api/v1/auth', fetcher)

    await expect(client.register({ name: 'A', email: 'a@b.com', password: 'x' })).rejects.toMatchObject({
      fieldErrors: [{ field: 'password', code: 'too_short' }],
    })
  })

  it('encodes identifiers placed in the path', async () => {
    const fetcher = vi.fn().mockResolvedValue(new Response(null, { status: 204 }))
    const client = createPortalClient('/api/v1/staff', fetcher)

    await client.revokeSession('../../admin')

    expect(fetcher.mock.calls[0]?.[0]).toBe('/api/v1/staff/sessions/..%2F..%2Fadmin')
  })
})
