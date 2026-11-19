import { execFileSync } from 'node:child_process'
import { resolve } from 'node:path'

import { expect, test } from '@playwright/test'

const PASSWORD = 'cavalo correto bateria grampo'

function inviteOwner(slug: string) {
  const email = `pedidos-${Date.now()}@${slug}.test`
  const output = execFileSync('docker', ['compose', 'run', '--rm', '--no-deps', '-T', 'tenantctl', 'staff', 'invite',
    '--slug', slug, '--email', email, '--name', 'Equipe', '--role', 'owner'], {
    cwd: resolve(import.meta.dirname, '../..'), encoding: 'utf8', stdio: ['ignore', 'pipe', 'ignore'],
  })
  return { email, link: output.match(/https?:\/\/\S+convite\?token=\S+/)![0] }
}

test('a visitor buys a product and the store ships the order', async ({ page, browser }) => {
  await page.goto('http://terra.localhost:8080/')
  await page.getByRole('link', { name: /Café Especial Torra Média/ }).first().click()
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Café Especial Torra Média')

  await page.getByRole('button', { name: '500 g' }).click()
  await page.getByRole('button', { name: 'Adicionar ao carrinho' }).click()
  await expect(page.getByRole('status')).toContainText('Produto adicionado')
  await expect(page.getByTestId('cart-count')).toContainText('1')

  await page.getByRole('link', { name: 'Ver carrinho' }).click()
  await expect(page.getByText('Café Especial Torra Média')).toBeVisible()
  await page.getByRole('button', { name: 'Finalizar compra' }).click()

  await page.getByLabel('Nome completo').fill('Ana Compradora')
  await page.getByLabel('E-mail').fill(`ana-${Date.now()}@exemplo.test`)
  await page.getByLabel('Destinatário').fill('Ana Compradora')
  await page.getByLabel('CEP').fill('01310-100')
  await page.getByLabel('Estado').selectOption('SP')
  await page.getByLabel('Rua').fill('Avenida Paulista')
  await page.getByLabel('Número').fill('1000')
  await page.getByLabel('Bairro').fill('Bela Vista')
  await page.getByLabel('Cidade').fill('São Paulo')
  await expect(page.getByTestId('checkout-total')).toContainText('89,90')
  await page.getByRole('radio', { name: /Entrega padrão/ }).check()
  await expect(page.getByTestId('checkout-total')).toContainText('104,90')
  await page.getByRole('button', { name: 'Confirmar pedido' }).click()

  await expect(page.getByRole('heading', { level: 1 })).toHaveText(/Pedido \d+/)
  const orderNumber = (await page.getByRole('heading', { level: 1 }).textContent())!.replace(/\D/g, '')
  await expect(page.getByTestId('order-status')).toHaveText('Aguardando pagamento')
  await page.getByRole('button', { name: 'Aprovar pagamento de teste' }).click()
  await expect(page.getByTestId('order-status')).toHaveText('Pago')
  await expect(page.getByTestId('cart-count')).toContainText('0')

  const owner = inviteOwner('terra')
  const context = await browser.newContext()
  const admin = await context.newPage()
  await admin.goto(owner.link)
  await admin.getByLabel('Seu nome').fill('Equipe Terra')
  await admin.getByLabel('Nova senha', { exact: true }).fill(PASSWORD)
  await admin.getByLabel('Confirme a nova senha').fill(PASSWORD)
  await admin.getByRole('button', { name: 'Salvar senha' }).click()
  await admin.getByLabel('Identificador da loja').fill('terra')
  await admin.getByLabel('E-mail').fill(owner.email)
  await admin.getByLabel('Senha').fill(PASSWORD)
  await admin.getByRole('button', { name: 'Entrar' }).click()

  await admin.getByRole('link', { name: 'Pedidos' }).click()
  await admin.getByRole('link', { name: `Pedido ${orderNumber}` }).click()
  await expect(admin.getByTestId('order-status')).toHaveText('Pago')
  await admin.getByRole('button', { name: 'Iniciar separação' }).click()
  await expect(admin.getByTestId('order-status')).toHaveText('Em separação')
  await admin.getByLabel('Código de rastreio').fill('BR123456789BR')
  await admin.getByRole('button', { name: 'Marcar como enviado' }).click()
  await expect(admin.getByTestId('order-status')).toHaveText('Enviado')
  await context.close()

  await page.reload()
  await expect(page.getByTestId('order-status')).toHaveText('Enviado')
  await expect(page.getByText('BR123456789BR')).toBeVisible()
})

test('product pages are rendered on the server with structured data', async ({ page }) => {
  const response = await page.goto('http://aurora.localhost:8080/p/serum-facial-vitamina-c')
  const html = (await response?.text()) ?? ''

  expect(response?.status()).toBe(200)
  expect(html).toContain('Sérum Facial Vitamina C')
  expect(html).toContain('"@type":"Product"')
  expect(html).toContain('"priceCurrency":"BRL"')
  expect(html).not.toContain('costPrice')
})

test('a product of one store does not exist in another', async ({ page }) => {
  const response = await page.goto('http://terra.localhost:8080/p/serum-facial-vitamina-c')

  expect(response?.status()).toBe(404)
})
