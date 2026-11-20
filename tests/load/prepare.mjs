// Prepares the stores used by the load test: a catalog of 20 products, one of
// them with enough stock for every order, and a flat delivery option.
// It runs only against the local environment and skips stores already prepared.
import { execFileSync } from 'node:child_process'
import { randomBytes } from 'node:crypto'
import { request } from 'node:http'

const stores = process.argv.slice(2)

// Waits and tries again when the API limits the rate, as a polite client does.
async function call(host, method, path, body, token) {
  for (let attempt = 0; ; attempt++) {
    try {
      return await send(host, method, path, body, token)
    } catch (error) {
      if (!/ -> 429 /.test(error.message) || attempt >= 12) throw error
      await new Promise((done) => setTimeout(done, 10_000))
    }
  }
}

function send(host, method, path, body, token) {
  return new Promise((resolve, reject) => {
    const payload = body === undefined ? undefined : JSON.stringify(body)
    const req = request({
      host: '127.0.0.1', port: 8080, method, path: `/api/v1${path}`,
      headers: {
        host, origin: `http://${host}:8080`, accept: 'application/json',
        ...(payload ? { 'content-type': 'application/json', 'content-length': Buffer.byteLength(payload) } : {}),
        ...(token ? { authorization: `Bearer ${token}` } : {}),
      },
    }, (res) => {
      let text = ''
      res.on('data', (chunk) => (text += chunk))
      res.on('end', () => (res.statusCode >= 400 ? reject(new Error(`${method} ${path} -> ${res.statusCode} ${text}`)) : resolve(text ? JSON.parse(text) : undefined)))
    })
    req.on('error', reject)
    if (payload) req.write(payload)
    req.end()
  })
}

const ctl = (...args) => execFileSync('docker', ['compose', 'run', '--rm', '--no-deps', '-T', 'tenantctl', ...args], { encoding: 'utf8', stdio: ['ignore', 'pipe', 'inherit'] })

const kinds = ['Caneca', 'Camiseta', 'Café', 'Presente', 'Caderno']
for (const slug of stores) {
  try {
    ctl('tenant', 'exists', '--slug', slug)
  } catch {
    ctl('tenant', 'create', '--slug', slug, '--name', `Loja de Carga ${slug.slice(-1)}`, '--plan', 'enterprise')
  }
  const host = `${slug}.localhost`
  if ((await call(host, 'GET', '/catalog/products')).items.length > 0) {
    console.log(`${slug} já preparada`)
    continue
  }
  const email = `carga@${slug}.localhost`
  const password = randomBytes(18).toString('base64url')
  const token = ctl('staff', 'invite', '--slug', slug, '--email', email, '--name', 'Carga', '--role', 'owner').match(/token=(\S+)/)[1]
  await call('admin.localhost', 'POST', '/staff/invitation/accept', { token, name: 'Carga', password })
  const { accessToken } = await call('admin.localhost', 'POST', '/staff/login', { store: slug, email, password })
  const admin = (method, path, body) => call('admin.localhost', method, `/staff${path}`, body, accessToken)

  const products = [{ name: 'Estoque de Carga', stock: 100_000 }]
  for (let i = 1; i < 20; i++) products.push({ name: `${kinds[i % kinds.length]} modelo ${i}`, stock: 500 })
  for (const [index, product] of products.entries()) {
    const { id } = await admin('POST', '/catalog/products', {
      name: product.name, status: 'active', description: `Produto ${index} para teste de carga, com descrição pesquisável.`,
      images: [], variants: [{ sku: `CARGA-${index}`, price: 2990 + index * 100, options: {} }],
    })
    const created = await admin('GET', `/catalog/products/${id}`)
    await admin('POST', '/inventory/adjustments', { variantId: created.variants[0].id, delta: product.stock, reason: 'Teste de carga' })
  }
  await admin('POST', '/shipping/methods', { name: 'Entrega padrão', kind: 'flat', price: 1500, minDays: 3, maxDays: 7, active: true })
  console.log(`${slug} preparada`)
}
