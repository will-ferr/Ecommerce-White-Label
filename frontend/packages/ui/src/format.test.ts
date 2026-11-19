import { describe, expect, it } from 'vitest'

import { centsToInput, formatMoney, orderStatusLabel, parseMoney, paymentStatusLabel } from './format'

describe('money', () => {
  it('formats cents as Brazilian currency', () => {
    expect(formatMoney(4990).replace(/\s/g, ' ')).toBe('R$ 49,90')
    expect(formatMoney(129900).replace(/\s/g, ' ')).toBe('R$ 1.299,00')
  })

  it.each([
    ['49,90', 4990],
    ['1.299,00', 129900],
    ['R$ 10', 1000],
    ['0,5', 50],
    ['12.5', 1250],
  ])('reads %s as %d cents', (text, cents) => {
    expect(parseMoney(text)).toBe(cents)
  })

  it.each(['', 'abc', '1,234,5', '-10', '10,999'])('refuses %s', (text) => {
    expect(parseMoney(text)).toBeNull()
  })

  it('round trips through the input format', () => {
    expect(parseMoney(centsToInput(4990))).toBe(4990)
    expect(centsToInput(null)).toBe('')
  })
})

describe('orderStatusLabel', () => {
  it('translates statuses and keeps unknown ones', () => {
    expect(orderStatusLabel('pending_payment')).toBe('Aguardando pagamento')
    expect(orderStatusLabel('unknown')).toBe('unknown')
  })
})

describe('paymentStatusLabel', () => {
  it('translates payment statuses', () => {
    expect(paymentStatusLabel('captured')).toBe('aprovado')
    expect(paymentStatusLabel('refunded')).toBe('estornado')
  })
})
