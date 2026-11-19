import { ApiError, NETWORK_ERROR, readResponse } from './index'
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
  headers?: Record<string, string>
}

const REFRESH_RETRY_DELAY_MS = 300

/**
 * Client of one portal. The access token stays only in memory, and the refresh
 * token lives in an HttpOnly cookie that scripts cannot read.
 */
export function createPortalClient(basePath: PortalBase, fetcher: typeof globalThis.fetch = (...args) => globalThis.fetch(...args)) {
  let accessToken: string | undefined
  let renewing: Promise<boolean> | undefined

  async function request(method: string, path: string, body: unknown, token: string | undefined, extra: Record<string, string> = {}): Promise<Response> {
    const url = path.startsWith('/api/') ? path : `${basePath}${path}`
    const headers: Record<string, string> = { Accept: 'application/json', ...extra }
    if (body !== undefined) headers['Content-Type'] = 'application/json'
    if (token) headers.Authorization = `Bearer ${token}`
    try {
      return await fetcher(url, {
        method,
        headers,
        credentials: 'same-origin',
        body: body === undefined ? undefined : JSON.stringify(body),
      })
    } catch {
      throw new ApiError(0, NETWORK_ERROR, 'Não foi possível conectar ao servidor.')
    }
  }

  const read = readResponse

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

  async function send<T>(method: string, path: string, { body, auth = false, headers }: SendOptions = {}): Promise<T> {
    const token = auth ? accessToken : undefined
    const response = await request(method, path, body, token, headers)
    // Only a request that carried a token can be fixed by renewing the session.
    if (token && response.status === 401 && (await renew())) {
      return read<T>(await request(method, path, body, accessToken, headers))
    }
    return read<T>(response)
  }

  const post = <T = void>(path: string, body?: unknown) => send<T>('POST', path, { body: body ?? {} })
  const authed = <T = void>(method: string, path: string, body?: unknown) => send<T>(method, path, { body, auth: true })
  const list = async <T>(path: string) => (await authed<Items<T>>('GET', path)).items

  return {
    /** Sends any request with the session of this portal, when there is one. */
    call: <T>(method: string, path: string, body?: unknown, headers?: Record<string, string>) =>
      send<T>(method, path, { body, auth: true, headers }),
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
    /** Downloads a file, such as a CSV export, with the session of this portal. */
    download: async (path: string): Promise<Blob> => {
      const token = accessToken
      let response = await request('GET', path, undefined, token)
      if (token && response.status === 401 && (await renew())) {
        response = await request('GET', path, undefined, accessToken)
      }
      if (!response.ok) await read(response)
      return response.blob()
    },
  }
}

export type PortalClient = ReturnType<typeof createPortalClient>
