import { expect, test, type Page } from '@playwright/test'

import { enrollAuthenticator, mailLink, PASSWORD, tenantctl, totp, unique } from './support'

/** Signs in a new platform operator, enrolling the authenticator. */
async function signInOperator(page: Page) {
  const operator = unique('operador')
  await page.goto(tenantctl('operator', 'invite', '--email', operator, '--name', 'Operadora'))
  await page.getByLabel('Seu nome').fill('Operadora')
  await page.getByLabel('Nova senha', { exact: true }).fill(PASSWORD)
  await page.getByLabel('Confirme a nova senha').fill(PASSWORD)
  await page.getByRole('button', { name: 'Salvar senha' }).click()
  await page.getByLabel('E-mail').fill(operator)
  await page.getByLabel('Senha').fill(PASSWORD)
  await page.getByRole('button', { name: 'Entrar' }).click()
  const secret = await enrollAuthenticator(page)
  await page.getByLabel('Código de verificação').fill(totp(secret))
  await page.getByRole('button', { name: 'Ativar e entrar' }).click()
  await page.getByRole('button', { name: 'Já guardei os códigos' }).click()
}

test('an operator creates a store and its owner runs it without database access', async ({ page, browser }) => {
  test.setTimeout(120_000)
  const slug = `e2e${Date.now().toString(36)}`
  const ownerEmail = unique('dona')
  const store = `http://${slug}.localhost:8080`

  // The platform creates the store and invites its owner from the console.
  await signInOperator(page)
  await page.getByRole('link', { name: 'Lojas' }).click()
  await page.getByLabel('Nome da loja').fill('Loja Ponta a Ponta')
  await page.getByLabel('Identificador').fill(slug)
  await page.getByRole('button', { name: 'Criar loja' }).click()
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Loja Ponta a Ponta')
  await expect(page.getByTestId('store-status')).toHaveText('Ativa')
  await page.getByLabel('Nome', { exact: true }).fill('Dona da Loja')
  await page.getByLabel('E-mail').fill(ownerEmail)
  await page.getByRole('button', { name: 'Enviar convite' }).click()
  await expect(page.getByRole('status')).toContainText(`Convite enviado para ${ownerEmail}`)

  // The owner accepts the invitation and sets up the store.
  const admin = await (await browser.newContext()).newPage()
  await admin.goto(await mailLink(ownerEmail, '/convite'))
  await admin.getByLabel('Seu nome').fill('Dona da Loja')
  await admin.getByLabel('Nova senha', { exact: true }).fill(PASSWORD)
  await admin.getByLabel('Confirme a nova senha').fill(PASSWORD)
  await admin.getByRole('button', { name: 'Salvar senha' }).click()
  await admin.getByLabel('Identificador da loja').fill(slug)
  await admin.getByLabel('E-mail').fill(ownerEmail)
  await admin.getByLabel('Senha').fill(PASSWORD)
  await admin.getByRole('button', { name: 'Entrar' }).click()

  await admin.getByRole('link', { name: 'Loja', exact: true }).click()
  await admin.getByLabel('Permitir que buscadores indexem a loja').check()
  await admin.getByRole('button', { name: 'Salvar' }).click()
  await expect(admin.getByRole('status')).toHaveText('Alterações salvas.')

  await admin.getByRole('link', { name: 'Aparência' }).click()
  await admin.getByLabel('Cor principal', { exact: true }).fill('#eeeeee')
  await admin.getByRole('button', { name: 'Publicar aparência' }).click()
  await expect(admin.getByText('Contraste insuficiente para leitura (mínimo 4,5:1).').first()).toBeVisible()
  await admin.getByLabel('Cor principal', { exact: true }).fill('#5a1a8a')
  await admin.getByRole('button', { name: 'Publicar aparência' }).click()
  await expect(admin.getByRole('status')).toContainText('Versão 2 publicada')

  await admin.getByRole('link', { name: 'Frete' }).click()
  await admin.getByLabel('Nome').fill('Entrega padrão')
  await admin.getByLabel('Preço (R$)').fill('15,00')
  await admin.getByRole('button', { name: 'Adicionar forma de entrega' }).click()
  await expect(admin.getByText('R$ 15,00 · 3 a 7 dias')).toBeVisible()

  await admin.getByRole('link', { name: 'Produtos' }).click()
  await admin.getByRole('link', { name: 'Novo produto' }).click()
  await admin.getByLabel('Nome', { exact: true }).fill('Caneca Branca')
  await admin.getByLabel('Situação').selectOption('active')
  await admin.getByLabel('SKU').fill('CANECA')
  await admin.getByLabel('Preço (R$)', { exact: true }).fill('39,90')
  await admin.getByRole('button', { name: 'Salvar produto' }).click()
  await admin.getByLabel('Ajuste de CANECA').fill('10')
  await admin.getByRole('button', { name: 'Aplicar' }).click()
  await expect(admin.getByTestId('stock-CANECA')).toContainText('10 disponíveis')

  await admin.getByRole('link', { name: 'Cupons' }).click()
  await admin.getByLabel('Código').fill('PRIMEIRA')
  await admin.getByRole('button', { name: 'Criar cupom' }).click()
  await expect(admin.getByText('10% de desconto · 0 usos')).toBeVisible()

  await admin.getByRole('link', { name: 'Páginas' }).click()
  await admin.getByRole('button', { name: 'Nova página' }).click()
  await admin.getByLabel('Título').fill('Sobre nós')
  await admin.getByLabel('Conteúdo').fill('## Nossa história\n\nCanecas feitas **à mão**.')
  await admin.getByLabel('Publicada').check()
  await admin.getByRole('button', { name: 'Salvar página' }).click()
  await expect(admin.getByText('/paginas/sobre-nos')).toBeVisible()

  // A buyer finds the store with its own identity, reads the page and buys with the coupon.
  await page.goto(`${store}/`)
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Loja Ponta a Ponta')
  expect(await page.locator('main [role="img"]').first().evaluate((el) => getComputedStyle(el).backgroundColor)).toBe('rgb(90, 26, 138)')
  await page.getByRole('link', { name: 'Sobre nós' }).click()
  await expect(page.getByRole('heading', { level: 2 })).toHaveText('Nossa história')
  const robots = await (await page.request.get(`http://127.0.0.1:8080/robots.txt`, { headers: { host: `${slug}.localhost` } })).text()
  expect(robots).toContain('Sitemap:')

  await page.goto(`${store}/p/caneca-branca`)
  await page.getByRole('button', { name: 'Adicionar ao carrinho' }).click()
  await page.getByRole('link', { name: 'Ver carrinho' }).click()
  await page.getByRole('button', { name: 'Finalizar compra' }).click()
  await page.getByLabel('Nome completo').fill('Bia Compradora')
  await page.getByLabel('E-mail').fill(unique('bia'))
  await page.getByLabel('Destinatário').fill('Bia Compradora')
  await page.getByLabel('CEP').fill('01310-100')
  await page.getByLabel('Estado').selectOption('SP')
  await page.getByLabel('Rua').fill('Avenida Paulista')
  await page.getByLabel('Número').fill('1000')
  await page.getByLabel('Bairro').fill('Bela Vista')
  await page.getByLabel('Cidade').fill('São Paulo')
  await page.getByLabel('Cupom de desconto').fill('primeira')
  await page.getByRole('button', { name: 'Aplicar' }).click()
  await expect(page.getByTestId('checkout-discount')).toContainText('3,99')
  await expect(page.getByTestId('checkout-total')).toContainText('50,91')
  await page.getByRole('button', { name: 'Confirmar pedido' }).click()
  await page.getByRole('button', { name: 'Aprovar pagamento de teste' }).click()
  await expect(page.getByTestId('order-status')).toHaveText('Pago')

  // The owner sees the sale in the report.
  await admin.getByRole('link', { name: 'Relatórios' }).click()
  await expect(admin.getByTestId('tile-Receita')).toHaveText('R$ 50,91')
  await expect(admin.getByTestId('tile-Descontos concedidos')).toHaveText('R$ 3,99')
})

test('a customer downloads and then erases their data', async ({ page }) => {
  const email = unique('privacidade')
  await page.goto('http://aurora.localhost:8080/cadastro')
  await page.getByLabel('Nome').fill('Rui Privado')
  await page.getByLabel('E-mail').fill(email)
  await page.getByLabel('Senha').fill(PASSWORD)
  await page.getByRole('button', { name: 'Criar conta' }).click()
  await page.goto(await mailLink(email, '/verificar-email'))
  await page.goto('http://aurora.localhost:8080/entrar')
  await page.getByLabel('E-mail').fill(email)
  await page.getByLabel('Senha').fill(PASSWORD)
  await page.getByRole('button', { name: 'Entrar' }).click()
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Olá, Rui Privado')

  const download = page.waitForEvent('download')
  await page.getByRole('button', { name: 'Baixar meus dados' }).click()
  const file = await download
  expect(file.suggestedFilename()).toBe('meus-dados.json')

  await page.getByRole('button', { name: 'Excluir minha conta' }).click()
  await page.getByLabel('Senha para confirmar a exclusão').fill('senha errada de proposito')
  await page.getByRole('button', { name: 'Excluir definitivamente' }).click()
  await expect(page.getByRole('alert')).toContainText('Senha incorreta')
  await page.getByLabel('Senha para confirmar a exclusão').fill(PASSWORD)
  await page.getByRole('button', { name: 'Excluir definitivamente' }).click()
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Aurora Cosméticos')

  await page.goto('http://aurora.localhost:8080/entrar')
  await page.getByLabel('E-mail').fill(email)
  await page.getByLabel('Senha').fill(PASSWORD)
  await page.getByRole('button', { name: 'Entrar' }).click()
  await expect(page.getByRole('alert')).toContainText('E-mail ou senha incorretos')
})
