/** Formats an amount kept in cents, such as 4990 as R$ 49,90. */
export function formatMoney(cents: number, currency = 'BRL', locale = 'pt-BR'): string {
  return new Intl.NumberFormat(locale, { style: 'currency', currency }).format(cents / 100)
}

/** Reads a price typed by a person, such as "49,90" or "1.299,00", into cents. */
export function parseMoney(text: string): number | null {
  const cleaned = text.replace(/[^\d,.-]/g, '').trim()
  if (!cleaned) return null
  const normalized = cleaned.includes(',') ? cleaned.replace(/\./g, '').replace(',', '.') : cleaned
  if (!/^\d+(\.\d{1,2})?$/.test(normalized)) return null
  return Math.round(Number(normalized) * 100)
}

/** Shows cents as an editable price, such as 4990 as "49,90". */
export function centsToInput(cents: number | null | undefined): string {
  if (cents === null || cents === undefined) return ''
  return (cents / 100).toFixed(2).replace('.', ',')
}

export const ORDER_STATUS_LABELS: Record<string, string> = {
  pending_payment: 'Aguardando pagamento',
  paid: 'Pago',
  processing: 'Em separação',
  shipped: 'Enviado',
  delivery_failed: 'Falha na entrega',
  delivered: 'Entregue',
  completed: 'Concluído',
  canceled: 'Cancelado',
}

export function orderStatusLabel(status: string): string {
  return ORDER_STATUS_LABELS[status] ?? status
}

export const PAYMENT_STATUS_LABELS: Record<string, string> = {
  pending: 'pendente',
  captured: 'aprovado',
  failed: 'recusado',
  canceled: 'cancelado',
  refund_pending: 'estorno em andamento',
  refunded: 'estornado',
}

export function paymentStatusLabel(status: string): string {
  return PAYMENT_STATUS_LABELS[status] ?? status
}

export function orderStatusTone(status: string): 'success' | 'warning' | 'danger' {
  if (status === 'canceled' || status === 'delivery_failed') return 'danger'
  if (status === 'pending_payment') return 'warning'
  return 'success'
}

export const STATES = [
  'AC', 'AL', 'AP', 'AM', 'BA', 'CE', 'DF', 'ES', 'GO', 'MA', 'MT', 'MS', 'MG', 'PA', 'PB', 'PR',
  'PE', 'PI', 'RJ', 'RN', 'RS', 'RO', 'RR', 'SC', 'SP', 'SE', 'TO',
]
