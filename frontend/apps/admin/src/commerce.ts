import { createCommerceAdminClient } from '@whitelabel/api-client'
import { useAuth } from '@whitelabel/panel-shell'
import { formatMoney } from '@whitelabel/ui'

/** Management API of the store, sent with the session of the team member. */
export function useCommerce() {
  const auth = useAuth()
  return createCommerceAdminClient((method, path, body, headers) => auth.client().call(method, `/api/v1/staff${path}`, body, headers))
}

export const money = (cents: number) => formatMoney(cents)

export const formatDate = (value: string) =>
  new Intl.DateTimeFormat('pt-BR', { dateStyle: 'short', timeStyle: 'short' }).format(new Date(value))

export const PRODUCT_STATUS_LABELS: Record<string, string> = { draft: 'Rascunho', active: 'Ativo', archived: 'Arquivado' }
