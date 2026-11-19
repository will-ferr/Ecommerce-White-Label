import { createCommerceAdminClient, createStoreManagementClient } from '@whitelabel/api-client'
import { useAuth } from '@whitelabel/panel-shell'
import { formatMoney } from '@whitelabel/ui'

/** Management API of the store, sent with the session of the team member. */
export function useCommerce() {
  const auth = useAuth()
  return createCommerceAdminClient((method, path, body, headers) => auth.client().call(method, `/api/v1/staff${path}`, body, headers))
}

/** Store settings, promotions, pages, reports and customers. */
export function useManagement() {
  const auth = useAuth()
  return createStoreManagementClient((method, path, body, headers) => auth.client().call(method, `/api/v1/staff${path}`, body, headers))
}

export const money = (cents: number) => formatMoney(cents)

export const formatDate = (value: string) =>
  new Intl.DateTimeFormat('pt-BR', { dateStyle: 'short', timeStyle: 'short' }).format(new Date(value))

export const PRODUCT_STATUS_LABELS: Record<string, string> = { draft: 'Rascunho', active: 'Ativo', archived: 'Arquivado' }

export const formatDay = (value: string) =>
  new Intl.DateTimeFormat('pt-BR', { dateStyle: 'short', timeZone: 'UTC' }).format(new Date(`${value}T00:00:00Z`))

/** Maps field errors of the API to the field names of a form. */
export function fieldMessages(error: unknown, labels: Record<string, string> = {}): Record<string, string> {
  const fields = (error as { fieldErrors?: { field: string; message: string }[] }).fieldErrors ?? []
  return Object.fromEntries(fields.map((field) => [field.field, labels[field.field] ?? 'Confira este campo.']))
}
