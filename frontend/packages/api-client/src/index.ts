import type { components } from './schema'

export type SystemStatus = components['schemas']['SystemStatus']
export type SystemState = components['schemas']['SystemState']
export type Problem = components['schemas']['Problem']
export type FieldError = components['schemas']['FieldError']

export const NETWORK_ERROR = 'NETWORK_ERROR'
export const UNEXPECTED_RESPONSE = 'UNEXPECTED_RESPONSE'

/** Failure of an API call, always safe to inspect in a single format. */
export class ApiError extends Error {
  readonly status: number
  readonly code: string
  readonly correlationId?: string
  readonly fieldErrors: FieldError[]

  constructor(status: number, code: string, message: string, correlationId?: string, fieldErrors: FieldError[] = []) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code
    this.correlationId = correlationId
    this.fieldErrors = fieldErrors
  }
}

export interface ApiClientOptions {
  /** Empty in the browser, because the API shares the origin of the store. */
  baseUrl?: string
  fetch?: typeof globalThis.fetch
}

interface RequestOptions {
  signal?: AbortSignal
  /** Non-2xx statuses whose body is a regular response of the endpoint. */
  acceptedStatuses?: number[]
}

function isProblem(body: unknown): body is Problem {
  return typeof body === 'object' && body !== null && 'code' in body && 'status' in body
}

async function readJson(response: Response): Promise<unknown> {
  try {
    return await response.json()
  } catch {
    return undefined
  }
}

export function createApiClient(options: ApiClientOptions = {}) {
  const baseUrl = options.baseUrl ?? ''
  const fetcher = options.fetch ?? globalThis.fetch.bind(globalThis)

  async function request<T>(path: string, { signal, acceptedStatuses = [] }: RequestOptions = {}): Promise<T> {
    let response: Response
    try {
      response = await fetcher(`${baseUrl}${path}`, {
        headers: { Accept: 'application/json' },
        credentials: 'same-origin',
        signal,
      })
    } catch (cause) {
      if (cause instanceof DOMException && cause.name === 'AbortError') throw cause
      throw new ApiError(0, NETWORK_ERROR, 'Não foi possível conectar ao servidor.')
    }

    const body = await readJson(response)
    if ((response.ok || acceptedStatuses.includes(response.status)) && body !== undefined) {
      return body as T
    }
    if (isProblem(body)) {
      throw new ApiError(
        response.status,
        body.code,
        body.detail ?? body.title,
        body.correlation_id,
        body.errors ?? [],
      )
    }
    throw new ApiError(response.status, UNEXPECTED_RESPONSE, 'O servidor retornou uma resposta inesperada.')
  }

  return {
    getStatus: (signal?: AbortSignal) =>
      request<SystemStatus>('/api/v1/status', { signal, acceptedStatuses: [503] }),
  }
}

export type ApiClient = ReturnType<typeof createApiClient>
