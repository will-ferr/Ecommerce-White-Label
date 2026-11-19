import { createConsoleClient } from '@whitelabel/api-client'
import { useAuth } from '@whitelabel/panel-shell'

/** Platform console API, sent with the session of the operator. */
export function useConsole() {
  const auth = useAuth()
  return createConsoleClient((method, path, body, headers) => auth.client().call(method, `/api/v1/platform${path}`, body, headers))
}

export const formatDate = (value: string) => new Intl.DateTimeFormat('pt-BR', { dateStyle: 'short' }).format(new Date(value))

export const STATUS_LABELS: Record<string, string> = { active: 'Ativa', suspended: 'Suspensa' }

export const FEATURE_LABELS: Record<string, string> = {
  reviews: 'Avaliações de produtos', wishlist: 'Lista de favoritos', loyalty: 'Programa de fidelidade',
  subscriptions: 'Produtos por assinatura', marketplace: 'Múltiplos vendedores',
}
