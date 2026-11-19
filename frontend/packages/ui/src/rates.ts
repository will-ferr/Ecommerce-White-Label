import { centsToInput, parseMoney } from './format'

/** One line of a shipping table, as the API expects it. */
export interface RateLine {
  postalCodeStart: string
  postalCodeEnd: string
  maxWeightGrams: number
  price: number
  minDays: number
  maxDays: number
}

export interface RateTable {
  rates: RateLine[]
  errors: string[]
}

const digits = (value: string) => value.replace(/\D/g, '')

const isWhole = (value: string) => /^\d+$/.test(value)

/**
 * Reads a shipping table written one rate per line, with fields separated by
 * semicolons: first CEP; last CEP; maximum weight in grams; price; minimum
 * days; maximum days. Spreadsheets export this format as CSV.
 */
export function parseRateTable(text: string): RateTable {
  const rates: RateLine[] = []
  const errors: string[] = []
  text.split(/\r?\n/).forEach((raw, index) => {
    const line = raw.trim()
    if (!line || line.startsWith('#')) return
    const fields = line.split(';').map((field) => field.trim())
    const label = `Linha ${index + 1}`
    if (fields.length !== 6) {
      errors.push(`${label}: use 6 campos separados por ponto e vírgula.`)
      return
    }
    const [start, end, weight, price, minDays, maxDays] = fields as [string, string, string, string, string, string]
    const postalCodeStart = digits(start)
    const postalCodeEnd = digits(end)
    const cents = parseMoney(price)
    if (postalCodeStart.length !== 8 || postalCodeEnd.length !== 8) {
      errors.push(`${label}: os CEPs precisam ter 8 dígitos.`)
    } else if (postalCodeStart > postalCodeEnd) {
      errors.push(`${label}: o CEP inicial é maior que o final.`)
    } else if (!isWhole(weight) || Number(weight) <= 0) {
      errors.push(`${label}: informe o peso máximo em gramas.`)
    } else if (cents === null) {
      errors.push(`${label}: informe um preço válido.`)
    } else if (!isWhole(minDays) || !isWhole(maxDays) || Number(minDays) > Number(maxDays)) {
      errors.push(`${label}: informe prazos em dias, com o mínimo menor ou igual ao máximo.`)
    } else {
      rates.push({
        postalCodeStart, postalCodeEnd, maxWeightGrams: Number(weight), price: cents,
        minDays: Number(minDays), maxDays: Number(maxDays),
      })
    }
  })
  if (!rates.length && !errors.length) errors.push('Adicione ao menos uma linha.')
  return { rates, errors }
}

/** Writes a shipping table in the format read by parseRateTable. */
export function formatRateTable(rates: RateLine[]): string {
  return rates
    .map((rate) => [rate.postalCodeStart, rate.postalCodeEnd, rate.maxWeightGrams, centsToInput(rate.price), rate.minDays, rate.maxDays].join(';'))
    .join('\n')
}
