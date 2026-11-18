import { ApiError, NETWORK_ERROR, type ApiClient } from '@whitelabel/api-client'
import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { useErrorStore } from './errors'
import { useSystemStore } from './system'

function clientWith(getStatus: ApiClient['getStatus']): ApiClient {
  return { getStatus, getStorefrontConfig: () => Promise.reject(new Error('not used')) }
}

beforeEach(() => setActivePinia(createPinia()))

describe('useSystemStore', () => {
  it('stores the platform status', async () => {
    const store = useSystemStore()

    await store.load(clientWith(async () => ({ status: 'degraded', version: '1.0.0' })))

    expect(store.state).toBe('degraded')
    expect(store.version).toBe('1.0.0')
  })

  it('exposes API failures to the screen', async () => {
    const store = useSystemStore()
    const failure = new ApiError(0, NETWORK_ERROR, 'Sem conexão.', 'corr-1')

    await store.load(clientWith(() => Promise.reject(failure)))

    expect(store.state).toBe('error')
    expect(store.errorMessage).toBe('Sem conexão.')
    expect(store.correlationId).toBe('corr-1')
  })

  it('clears a previous failure when retrying', async () => {
    const store = useSystemStore()
    await store.load(clientWith(() => Promise.reject(new ApiError(0, NETWORK_ERROR, 'Sem conexão.'))))

    await store.load(clientWith(async () => ({ status: 'ok', version: '1.0.0' })))

    expect(store.state).toBe('ok')
    expect(store.errorMessage).toBeUndefined()
  })

  it('does not hide programming errors', async () => {
    const store = useSystemStore()
    const bug = new TypeError('bug')

    await expect(store.load(clientWith(() => Promise.reject(bug)))).rejects.toBe(bug)
  })
})

describe('useErrorStore', () => {
  it('shows a generic message and never the raw error', () => {
    const log = vi.spyOn(console, 'error').mockImplementation(() => {})
    const store = useErrorStore()

    store.report(new Error('stack trace with internal details'))

    expect(store.message).not.toContain('internal details')
    expect(log).toHaveBeenCalledOnce()

    store.dismiss()
    expect(store.message).toBeUndefined()
  })
})
