import { describe, expect, it } from 'vitest'

import { formatRateTable, parseRateTable } from './rates'

describe('parseRateTable', () => {
  it('reads rates and ignores blank lines and comments', () => {
    const table = parseRateTable('# CEP inicial;CEP final;peso;preço;mín;máx\n01000-000;19999-999;1000;12,50;1;3\n\n20000000;99999999;5000;39,90;4;9\n')
    expect(table.errors).toEqual([])
    expect(table.rates).toEqual([
      { postalCodeStart: '01000000', postalCodeEnd: '19999999', maxWeightGrams: 1000, price: 1250, minDays: 1, maxDays: 3 },
      { postalCodeStart: '20000000', postalCodeEnd: '99999999', maxWeightGrams: 5000, price: 3990, minDays: 4, maxDays: 9 },
    ])
  })

  it('reports each invalid line by number', () => {
    const table = parseRateTable('01000000;19999999;1000;12,50;1\n99999999;01000000;1000;10;1;2\n01000000;19999999;0;10;1;2\n01000000;19999999;100;10;5;2')
    expect(table.rates).toEqual([])
    expect(table.errors).toEqual([
      'Linha 1: use 6 campos separados por ponto e vírgula.',
      'Linha 2: o CEP inicial é maior que o final.',
      'Linha 3: informe o peso máximo em gramas.',
      'Linha 4: informe prazos em dias, com o mínimo menor ou igual ao máximo.',
    ])
  })

  it('requires at least one rate', () => {
    expect(parseRateTable('  \n').errors).toEqual(['Adicione ao menos uma linha.'])
  })

  it('writes a table that reads back the same', () => {
    const rates = parseRateTable('01000000;19999999;1000;12,50;1;3').rates
    expect(parseRateTable(formatRateTable(rates)).rates).toEqual(rates)
  })
})
