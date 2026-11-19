import { execFileSync } from 'node:child_process'
import { createHmac } from 'node:crypto'
import { resolve } from 'node:path'

import { expect, type Page } from '@playwright/test'

export const PASSWORD = 'cavalo correto bateria grampo'
export const MAILPIT = 'http://127.0.0.1:8025/api/v1'

export function unique(prefix: string) {
  return `${prefix}-${Date.now()}-${Math.floor(Math.random() * 1e6)}@exemplo.test`
}

/** Computes an authenticator code without using the application code. */
export function totp(secret: string, offset = 0) {
  const alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567'
  let bits = ''
  for (const char of secret) bits += alphabet.indexOf(char).toString(2).padStart(5, '0')
  const key = Buffer.from(bits.match(/.{8}/g)!.map((byte) => parseInt(byte, 2)))
  const counter = Buffer.alloc(8)
  counter.writeBigUInt64BE(BigInt(Math.floor(Date.now() / 30000) + offset))
  const digest = createHmac('sha1', key).update(counter).digest()
  const start = digest[digest.length - 1]! & 0x0f
  return String((digest.readUInt32BE(start) & 0x7fffffff) % 1_000_000).padStart(6, '0')
}

export async function mailLink(to: string, path: string) {
  for (let attempt = 0; attempt < 40; attempt++) {
    const search = await (await fetch(`${MAILPIT}/search?query=${encodeURIComponent(`to:"${to}"`)}`)).json()
    for (const summary of search.messages ?? []) {
      const message = await (await fetch(`${MAILPIT}/message/${summary.ID}`)).json()
      const link = (message.Text as string).match(new RegExp(`https?://\\S+${path}\\?token=\\S+`))
      if (link) return link[0]
    }
    await new Promise((done) => setTimeout(done, 250))
  }
  throw new Error(`No e-mail with ${path} was delivered to ${to}`)
}

export function tenantctl(...args: string[]) {
  const output = execFileSync('docker', ['compose', 'run', '--rm', '--no-deps', '-T', 'tenantctl', ...args], {
    cwd: resolve(import.meta.dirname, '../..'),
    encoding: 'utf8',
    stdio: ['ignore', 'pipe', 'ignore'],
  })
  return output.match(/https?:\/\/\S+convite\?token=\S+/)![0]
}

export async function enrollAuthenticator(page: Page) {
  const secret = await page.getByTestId('totp-secret').getAttribute('data-secret')
  expect(secret).toBeTruthy()
  return secret!
}
