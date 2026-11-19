// Creates the demonstration catalog of the local stores through the public API.
// It runs only in development and CI, and skips stores that already have products.
import { execFileSync } from 'node:child_process'
import { randomBytes } from 'node:crypto'
import { request } from 'node:http'

const EDGE_PORT = 8080

function call(host, method, path, body, token) {
  return new Promise((resolve, reject) => {
    const payload = body === undefined ? undefined : JSON.stringify(body)
    const req = request(
      {
        host: '127.0.0.1', port: EDGE_PORT, method, path: `/api/v1${path}`,
        headers: {
          host, origin: `http://${host}:${EDGE_PORT}`, accept: 'application/json',
          ...(payload ? { 'content-type': 'application/json', 'content-length': Buffer.byteLength(payload) } : {}),
          ...(token ? { authorization: `Bearer ${token}` } : {}),
        },
      },
      (res) => {
        let text = ''
        res.on('data', (chunk) => (text += chunk))
        res.on('end', () => {
          const data = text ? JSON.parse(text) : undefined
          if (res.statusCode >= 400) reject(new Error(`${method} ${path} -> ${res.statusCode} ${text}`))
          else resolve(data)
        })
      },
    )
    req.on('error', reject)
    if (payload) req.write(payload)
    req.end()
  })
}

const reais = (value) => Math.round(value * 100)

const catalogs = {
  aurora: {
    categories: ['Skincare', 'Maquiagem', 'Corpo'],
    products: [
      {
        name: 'Sérum Facial Vitamina C', category: 'Skincare', featured: true, brand: 'Aurora',
        description: 'Sérum leve com vitamina C estabilizada.\nUso diário, pela manhã.',
        variants: [{ sku: 'SERUM-30', options: { Volume: '30 ml' }, price: 129.9 }, { sku: 'SERUM-50', options: { Volume: '50 ml' }, price: 189.9, compareAt: 209.9 }],
      },
      {
        name: 'Batom Cremoso', category: 'Maquiagem', featured: true, brand: 'Aurora',
        description: 'Acabamento cremoso e alta pigmentação.',
        variants: [
          { sku: 'BATOM-ROSE', options: { Cor: 'Rosé' }, price: 49.9 },
          { sku: 'BATOM-NUDE', options: { Cor: 'Nude' }, price: 49.9 },
          { sku: 'BATOM-VERMELHO', options: { Cor: 'Vermelho' }, price: 49.9, stock: 1 },
        ],
      },
      {
        name: 'Hidratante Corporal', category: 'Corpo', brand: 'Aurora',
        description: 'Hidratação por até 24 horas.',
        variants: [{ sku: 'HIDRAT-200', options: { Volume: '200 ml' }, price: 69.9 }],
      },
    ],
  },
  terra: {
    categories: ['Cafés', 'Acessórios'],
    products: [
      {
        name: 'Café Especial Torra Média', category: 'Cafés', featured: true, brand: 'Terra',
        description: 'Notas de chocolate e caramelo. Moído na hora do envio.',
        variants: [{ sku: 'CAFE-250', options: { Peso: '250 g' }, price: 49.9 }, { sku: 'CAFE-500', options: { Peso: '500 g' }, price: 89.9 }],
      },
      {
        name: 'Café em Grãos Bourbon', category: 'Cafés', brand: 'Terra',
        description: 'Grãos inteiros de café bourbon amarelo.',
        variants: [{ sku: 'BOURBON-500', options: { Peso: '500 g' }, price: 59.9 }],
      },
      {
        name: 'Prensa Francesa 600 ml', category: 'Acessórios', featured: true, brand: 'Terra',
        description: 'Vidro borossilicato e filtro de aço inox.',
        variants: [{ sku: 'PRENSA-600', options: { Capacidade: '600 ml' }, price: 149.9 }],
      },
    ],
  },
}

async function seed(slug, catalog) {
  const storeHost = `${slug}.localhost`
  const existing = await call(storeHost, 'GET', '/catalog/products')
  if (existing.items.length > 0) {
    console.log(`Catálogo de ${slug} já existe`)
    return
  }

  const email = `catalogo@${slug}.localhost`
  const password = randomBytes(18).toString('base64url')
  const output = execFileSync('docker', ['compose', 'run', '--rm', '--no-deps', '-T', 'tenantctl', 'staff', 'invite',
    '--slug', slug, '--email', email, '--name', 'Equipe de demonstração', '--role', 'owner'], { encoding: 'utf8', stdio: ['ignore', 'pipe', 'inherit'] })
  const token = output.match(/token=(\S+)/)?.[1]
  if (!token) throw new Error(`No invitation token for ${slug}`)

  const admin = 'admin.localhost'
  await call(admin, 'POST', '/staff/invitation/accept', { token, name: 'Equipe de demonstração', password })
  const { accessToken } = await call(admin, 'POST', '/staff/login', { store: slug, email, password })

  const categoryIds = {}
  for (const [position, name] of catalog.categories.entries()) {
    categoryIds[name] = (await call(admin, 'POST', '/staff/catalog/categories', { name, position }, accessToken)).id
  }
  for (const product of catalog.products) {
    const { id } = await call(admin, 'POST', '/staff/catalog/products', {
      name: product.name, status: 'active', categoryId: categoryIds[product.category], brand: product.brand,
      description: product.description, featured: product.featured ?? false, images: [],
      variants: product.variants.map((variant) => ({
        sku: variant.sku, options: variant.options, price: reais(variant.price),
        compareAtPrice: variant.compareAt ? reais(variant.compareAt) : null,
      })),
    }, accessToken)
    const created = await call(admin, 'GET', `/staff/catalog/products/${id}`, undefined, accessToken)
    for (const variant of created.variants) {
      const stock = product.variants.find((item) => item.sku === variant.sku)?.stock ?? 25
      await call(admin, 'POST', '/staff/inventory/adjustments', { variantId: variant.id, delta: stock, reason: 'Estoque inicial', minQuantity: 3 }, accessToken)
    }
  }
  await call(admin, 'POST', '/staff/shipping/methods', { name: 'Entrega padrão', kind: 'flat', price: 1500, minDays: 3, maxDays: 7, active: true }, accessToken)
  await call(admin, 'POST', '/staff/shipping/methods', { name: 'Retirada na loja', kind: 'pickup', price: 0, minDays: 0, maxDays: 1, active: true }, accessToken)
  console.log(`Catálogo de ${slug} criado. Painel: http://admin.localhost:8080 (loja ${slug}, ${email}, senha ${password})`)
}

for (const [slug, catalog] of Object.entries(catalogs)) {
  await seed(slug, catalog)
}
