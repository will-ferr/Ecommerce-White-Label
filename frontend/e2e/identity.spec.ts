import { expect, test } from '@playwright/test'

import { enrollAuthenticator, mailLink, PASSWORD, tenantctl, totp, unique } from './support'

test('a customer signs up, protects the account and signs in again', async ({ page }) => {
  const email = unique('cliente')

  await page.goto('http://aurora.localhost:8080/cadastro')
  await page.getByLabel('Nome').fill('Maria Teste')
  await page.getByLabel('E-mail').fill(email)
  await page.getByLabel('Senha').fill(PASSWORD)
  await page.getByRole('button', { name: 'Criar conta' }).click()
  await expect(page.getByRole('status')).toContainText('Cadastro recebido')

  await page.goto(await mailLink(email, '/verificar-email'))
  await expect(page.getByRole('status')).toContainText('E-mail confirmado')

  await page.goto('http://aurora.localhost:8080/entrar')
  await page.getByLabel('E-mail').fill(email)
  await page.getByLabel('Senha').fill('senha errada de proposito')
  await page.getByRole('button', { name: 'Entrar' }).click()
  await expect(page.getByRole('alert')).toContainText('E-mail ou senha incorretos')

  await page.getByLabel('Senha').fill(PASSWORD)
  await page.getByRole('button', { name: 'Entrar' }).click()
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Olá, Maria Teste')

  await page.reload()
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Olá, Maria Teste')

  const storage = await page.evaluate(() => JSON.stringify({ ...localStorage, ...sessionStorage }))
  expect(storage).not.toContain('eyJ')
  const cookies = await page.context().cookies()
  expect(cookies.find((cookie) => cookie.name === 'wl_refresh')?.httpOnly).toBe(true)

  await page.getByLabel('Confirme sua senha').fill(PASSWORD)
  await page.getByRole('button', { name: 'Configurar' }).click()
  const secret = await enrollAuthenticator(page)
  await page.getByLabel('Código de verificação').fill(totp(secret, -1))
  await page.getByRole('button', { name: 'Ativar' }).click()
  await expect(page.getByTestId('recovery-codes').getByRole('listitem')).toHaveCount(10)
  await page.getByRole('button', { name: 'Já guardei os códigos' }).click()

  await page.getByRole('button', { name: 'Sair' }).click()
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Aurora Cosméticos')

  await page.goto('http://aurora.localhost:8080/conta')
  await expect(page).toHaveURL(/\/entrar$/)
  await page.getByLabel('E-mail').fill(email)
  await page.getByLabel('Senha').fill(PASSWORD)
  await page.getByRole('button', { name: 'Entrar' }).click()
  await page.getByLabel('Código de verificação').fill(totp(secret))
  await page.getByRole('button', { name: 'Confirmar' }).click()
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Olá, Maria Teste')
})

test('a customer resets a forgotten password', async ({ page }) => {
  const email = unique('senha')
  await page.goto('http://terra.localhost:8080/cadastro')
  await page.getByLabel('Nome').fill('Rui Teste')
  await page.getByLabel('E-mail').fill(email)
  await page.getByLabel('Senha').fill(PASSWORD)
  await page.getByRole('button', { name: 'Criar conta' }).click()
  await expect(page.getByRole('status')).toContainText('Cadastro recebido')

  await page.goto('http://terra.localhost:8080/recuperar-senha')
  await page.getByLabel('E-mail').fill(email)
  await page.getByRole('button', { name: 'Enviar instruções' }).click()
  await expect(page.getByRole('status')).toContainText('Se houver uma conta')

  const newPassword = 'nova frase bem longa e segura'
  await page.goto(await mailLink(email, '/redefinir-senha'))
  await page.getByLabel('Nova senha', { exact: true }).fill(newPassword)
  await page.getByLabel('Confirme a nova senha').fill(newPassword)
  await page.getByRole('button', { name: 'Salvar senha' }).click()

  await expect(page).toHaveURL(/\/entrar$/)
  await page.getByLabel('E-mail').fill(email)
  await page.getByLabel('Senha').fill(newPassword)
  await page.getByRole('button', { name: 'Entrar' }).click()
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Olá, Rui Teste')
})

test('a store owner manages the team and a viewer stays restricted', async ({ page, browser }) => {
  const owner = unique('dono')
  const viewer = unique('leitor')

  await page.goto(tenantctl('staff', 'invite', '--slug', 'aurora', '--email', owner, '--name', 'Dona', '--role', 'owner'))
  await page.getByLabel('Seu nome').fill('Dona da Loja')
  await page.getByLabel('Nova senha', { exact: true }).fill(PASSWORD)
  await page.getByLabel('Confirme a nova senha').fill(PASSWORD)
  await page.getByRole('button', { name: 'Salvar senha' }).click()

  await page.getByLabel('Identificador da loja').fill('aurora')
  await page.getByLabel('E-mail').fill(owner)
  await page.getByLabel('Senha').fill(PASSWORD)
  await page.getByRole('button', { name: 'Entrar' }).click()
  await page.getByRole('link', { name: 'Equipe' }).click()

  await page.getByLabel('Nome').fill('Pessoa Leitora')
  await page.getByLabel('E-mail').fill(viewer)
  await page.getByLabel('Papel', { exact: true }).selectOption('viewer')
  await page.getByRole('button', { name: 'Enviar convite' }).click()
  await expect(page.getByRole('status')).toContainText('Convite enviado')

  const context = await browser.newContext()
  const viewerPage = await context.newPage()
  await viewerPage.goto(await mailLink(viewer, '/convite'))
  await viewerPage.getByLabel('Seu nome').fill('Pessoa Leitora')
  await viewerPage.getByLabel('Nova senha', { exact: true }).fill(PASSWORD)
  await viewerPage.getByLabel('Confirme a nova senha').fill(PASSWORD)
  await viewerPage.getByRole('button', { name: 'Salvar senha' }).click()
  await viewerPage.getByLabel('Identificador da loja').fill('aurora')
  await viewerPage.getByLabel('E-mail').fill(viewer)
  await viewerPage.getByLabel('Senha').fill(PASSWORD)
  await viewerPage.getByRole('button', { name: 'Entrar' }).click()
  await expect(viewerPage.getByRole('link', { name: 'Segurança' })).toBeVisible()
  await expect(viewerPage.getByRole('link', { name: 'Equipe' })).toHaveCount(0)
  await viewerPage.goto('http://admin.localhost:8080/equipe')
  await expect(viewerPage).toHaveURL('http://admin.localhost:8080/')

  await page.getByRole('link', { name: 'Auditoria' }).click()
  await expect(page.getByText('Pessoa convidada').first()).toBeVisible()
  await context.close()
})

test('a platform operator must enroll two-step verification', async ({ page }) => {
  const operator = unique('operador')

  await page.goto(tenantctl('operator', 'invite', '--email', operator, '--name', 'Operador'))
  await page.getByLabel('Seu nome').fill('Operador Teste')
  await page.getByLabel('Nova senha', { exact: true }).fill(PASSWORD)
  await page.getByLabel('Confirme a nova senha').fill(PASSWORD)
  await page.getByRole('button', { name: 'Salvar senha' }).click()

  await page.getByLabel('E-mail').fill(operator)
  await page.getByLabel('Senha').fill(PASSWORD)
  await page.getByRole('button', { name: 'Entrar' }).click()
  await expect(page.getByText('Esta conta exige verificação em duas etapas.')).toBeVisible()

  const secret = await enrollAuthenticator(page)
  await page.getByLabel('Código de verificação').fill(totp(secret))
  await page.getByRole('button', { name: 'Ativar e entrar' }).click()
  await expect(page.getByTestId('recovery-codes').getByRole('listitem')).toHaveCount(10)
  await page.getByRole('button', { name: 'Já guardei os códigos' }).click()
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Operação da plataforma')
})
