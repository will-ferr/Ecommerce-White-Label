import { ApiError, type PortalClient } from '@whitelabel/api-client'
import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { errorMessage } from './messages'
import { createAuthStore } from './store'

const user = {
  id: '1', email: 'a@b.com', name: 'Ana', realm: 'staff', emailVerified: true, mfaEnabled: false, permissions: ['team:read'],
}

function clientWith(overrides: Partial<PortalClient>): PortalClient {
  return overrides as PortalClient
}

beforeEach(() => {
  setActivePinia(createPinia())
  localStorage.clear()
})

describe('auth store', () => {
  it('does not call the API when no session may exist', async () => {
    const refresh = vi.fn()
    const store = createAuthStore(clientWith({ refresh }))()

    await store.restore()

    expect(refresh).not.toHaveBeenCalled()
    expect(store.isAuthenticated).toBe(false)
  })

  it('restores a session after a page load', async () => {
    localStorage.setItem('wl.session', '1')
    const store = createAuthStore(clientWith({ refresh: vi.fn().mockResolvedValue({ status: 'authenticated', user }) }))()

    await store.restore()

    expect(store.user?.email).toBe('a@b.com')
    expect(store.can('team:read')).toBe(true)
    expect(store.can('team:manage')).toBe(false)
  })

  it('forgets an expired session', async () => {
    localStorage.setItem('wl.session', '1')
    const failure = new ApiError(401, 'SESSION_EXPIRED', 'expired')
    const store = createAuthStore(clientWith({ refresh: vi.fn().mockRejectedValue(failure) }))()

    await store.restore()

    expect(store.isAuthenticated).toBe(false)
    expect(localStorage.getItem('wl.session')).toBeNull()
  })

  it('never keeps tokens in browser storage', () => {
    const store = createAuthStore(clientWith({}))()

    store.accept({ status: 'authenticated', accessToken: 'secret-token', user })

    expect(JSON.stringify({ ...localStorage })).not.toContain('secret-token')
    expect(JSON.stringify(store.$state)).not.toContain('secret-token')
  })

  it('clears the session on logout even if the request fails', async () => {
    const store = createAuthStore(clientWith({ logout: vi.fn().mockRejectedValue(new Error('offline')) }))()
    store.accept({ status: 'authenticated', user })

    await expect(store.logout()).rejects.toThrow('offline')

    expect(store.isAuthenticated).toBe(false)
  })
})

describe('errorMessage', () => {
  it('translates known failures', () => {
    expect(errorMessage(new ApiError(401, 'INVALID_CREDENTIALS', 'x'))).toBe('E-mail ou senha incorretos.')
  })

  it('translates field errors', () => {
    const error = new ApiError(400, 'VALIDATION_FAILED', 'x', undefined, [{ field: 'password', code: 'common', message: 'x' }])
    expect(errorMessage(error)).toContain('muito comum')
  })

  it('never shows raw technical messages', () => {
    expect(errorMessage(new Error('pq: connection refused'))).not.toContain('pq:')
    expect(errorMessage(new ApiError(500, 'INTERNAL_ERROR', 'stack trace'))).not.toContain('stack')
  })
})
