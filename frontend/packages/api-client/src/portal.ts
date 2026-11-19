import { ApiError, NETWORK_ERROR, UNEXPECTED_RESPONSE, type Problem } from './index'
import type { components } from './schema'

export type AuthResult = components['schemas']['AuthResult']
export type UserView = components['schemas']['UserView']
export type MfaEnrollment = components['schemas']['MfaEnrollment']
export type SessionView = components['schemas']['SessionView']
export type AccessEvent = components['schemas']['AccessEvent']
export type TeamMember = components['schemas']['TeamMember']
export type RoleView = components['schemas']['RoleView']
export type AuditEntry = components['schemas']['AuditEntry']

export type PortalBase = '/api/v1/auth' | '/api/v1/staff' | '/api/v1/platform'

interface Items<T> {
  items: T[]
}

interface SendOptions {
  body?: unknown
  /** Sends the access token and renews it once when it has expired. */
  auth?: boolean
}

const REFRESH_RETRY_DELAY_MS = 300

function isProblem(body: unknown): body is Problem {
  return typeof body === 'object' && body !== null && 'code' in body && 'status' in body
}

/**
 * Client of one portal. The access token stays only in memory, and the refresh
 * token lives in an HttpOnly cookie that scripts cannot read.
 */
export function createPortalClient(basePath: PortalBase, fetcher: typeof globalThis.fetch = (...args) => globalThis.fetch(...args)) {
  let accessToken: string | undefined
  let renewing: Promise<boolean> | undefined

  async function request(method: string, path: string, body: unknown, token: string | undefined): Promise<Response> {
    const headers: Record<string, string> = { Accept: 'application/json' }
    if (body !== undefined) headers['Content-Type'] = 'application/json'
    if (token) headers.Authorization = `Bearer ${token}`
    try {
      return await fetcher(`${basePath}${path}`, {
        method,
        headers,
        credentials: 'same-origin',
        body: body === undefined ? undefined : JSON.stringify(body),
      })
    } catch {
      throw new ApiError(0, NETWORK_ERROR, 'Não foi possível conectar ao servidor.')
    }
  }

  async function read<T>(response: Response): Promise<T> {
    if (response.status === 202 || response.status === 204) return undefined as T
    let payload: unknown
    try {
      payload = await response.json()
    } catch {
      throw new ApiError(response.status, UNEXPECTED_RESPONSE, 'O servidor retornou uma resposta inesperada.')
    }
    if (response.ok) return payload as T
    if (isProblem(payload)) {
      throw new ApiError(response.status, payload.code, payload.detail ?? payload.title, payload.correlation_id, payload.errors ?? [])
    }
    throw new ApiError(response.status, UNEXPECTED_RESPONSE, 'O servidor retornou uma resposta inesperada.')
  }

  function remember(result: AuthResult): AuthResult {
    if (result.accessToken) accessToken = result.accessToken
    return result
  }

  async function refreshOnce(): Promise<AuthResult> {
    const response = await request('POST', '/refresh', undefined, undefined)
    if (response.status === 409) {
      // Another tab has just renewed the session and updated the cookie.
      await new Promise((resolve) => setTimeout(resolve, REFRESH_RETRY_DELAY_MS))
      return remember(await read<AuthResult>(await request('POST', '/refresh', undefined, undefined)))
    }
    return remember(await read<AuthResult>(response))
  }

  function renew(): Promise<boolean> {
    renewing ??= refreshOnce()
      .then(() => true)
      .catch(() => {
        accessToken = undefined
        return false
      })
      .finally(() => {
        renewing = undefined
      })
    return renewing
  }

  async function send<T>(method: string, path: string, { body, auth = false }: SendOptions = {}): Promise<T> {
    const response = await request(method, path, body, auth ? accessToken : undefined)
    if (auth && response.status === 401 && (await renew())) {
      return read<T>(await request(method, path, body, accessToken))
    }
    return read<T>(response)
  }

  const post = <T = void>(path: string, body?: unknown) => send<T>('POST', path, { body: body ?? {} })
  const authed = <T = void>(method: string, path: string, body?: unknown) => send<T>(method, path, { body, auth: true })
  const list = async <T>(path: string) => (await authed<Items<T>>('GET', path)).items

  return {
    login: async (input: { email: string; password: string; store?: string }) => remember(await post<AuthResult>('/login', input)),
    completeChallenge: async (challenge: string, code: string) =>
      remember(await post<AuthResult>('/mfa/challenge', { challenge, code })),
    startSetup: (challenge: string) => post<MfaEnrollment>('/mfa/setup/start', { challenge }),
    confirmSetup: async (challenge: string, code: string) =>
      remember(await post<AuthResult>('/mfa/setup/confirm', { challenge, code })),
    refresh: refreshOnce,
    logout: async () => {
      await send<void>('POST', '/logout')
      accessToken = undefined
    },
    register: (input: { name: string; email: string; password: string }) => post('/register', input),
    forgotPassword: (input: { email: string; store?: string }) => post('/password/forgot', input),
    resetPassword: (token: string, password: string) => post('/password/reset', { token, password }),
    verifyEmail: (token: string) => post('/email/verify', { token }),
    acceptInvitation: (input: { token: string; name: string; password: string }) => post('/invitation/accept', input),

    me: () => authed<UserView>('GET', '/me'),
    changePassword: (currentPassword: string, newPassword: string) =>
      authed('POST', '/password/change', { currentPassword, newPassword }),
    listSessions: () => list<SessionView>('/sessions'),
    revokeSession: (id: string) => authed('DELETE', `/sessions/${encodeURIComponent(id)}`),
    revokeOtherSessions: () => authed('DELETE', '/sessions'),
    accessHistory: () => list<AccessEvent>('/access-history'),
    enrollMfa: (password: string) => authed<MfaEnrollment>('POST', '/mfa/enroll', { password }),
    confirmMfa: (code: string) => authed<{ recoveryCodes: string[] }>('POST', '/mfa/enroll/confirm', { code }),
    disableMfa: (password: string, code: string) => authed('POST', '/mfa/disable', { password, code }),
    regenerateRecoveryCodes: (code: string) => authed<{ recoveryCodes: string[] }>('POST', '/mfa/recovery-codes', { code }),

    listTeam: () => list<TeamMember>('/team'),
    listRoles: () => list<RoleView>('/roles'),
    inviteMember: (input: { name: string; email: string; role: string }) => authed('POST', '/team/invitations', input),
    changeRole: (id: string, role: string) => authed('PUT', `/team/${encodeURIComponent(id)}/role`, { role }),
    deactivateMember: (id: string) => authed('POST', `/team/${encodeURIComponent(id)}/deactivate`, {}),
    listAudit: () => list<AuditEntry>('/audit'),
  }
}

export type PortalClient = ReturnType<typeof createPortalClient>
