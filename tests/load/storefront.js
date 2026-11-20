// Load test of a store: buyers browsing the catalog and placing orders.
// Run it through scripts/load-test.sh, which prepares the store and the data.
//
// Each virtual user presents its own address from the range reserved for
// benchmarks (198.18.0.0/15, RFC 2544), so per-IP rate limits behave as they
// would with real clients. The API reads the rightmost untrusted address.
import http from 'k6/http'
import { check, fail, group } from 'k6'
import exec from 'k6/execution'
import { Counter, Trend } from 'k6/metrics'

// Load is spread over several stores, as on a shared platform: each store is
// also capped by its own rate limit (6000 requests per minute by default).
const STORES = (__ENV.STORE_HOSTS || 'teste-carga-1.localhost').split(',')
const BASE = __ENV.BASE_URL || 'http://edge:8080'
const PEAK_RPS = Number(__ENV.PEAK_RPS || 150)
const ORDERS_PER_SECOND = Number(__ENV.ORDERS_PER_SECOND || 3)
// Length of each load step, in seconds. The whole test lasts six steps.
const STEP = Number(__ENV.STEP_SECONDS || 60)

const ordersPlaced = new Counter('orders_placed')
const responses = new Counter('responses')
const checkoutDuration = new Trend('checkout_duration', true)

export const options = {
  scenarios: {
    // Browsing grows in steps to find where latency starts to degrade.
    browse: {
      executor: 'ramping-arrival-rate',
      exec: 'browse',
      startRate: 10,
      timeUnit: '1s',
      preAllocatedVUs: 50,
      maxVUs: 400,
      stages: [
        { target: Math.round(PEAK_RPS * 0.25), duration: `${STEP}s` },
        { target: Math.round(PEAK_RPS * 0.5), duration: `${STEP}s` },
        { target: Math.round(PEAK_RPS * 0.75), duration: `${STEP}s` },
        { target: PEAK_RPS, duration: `${STEP}s` },
        { target: PEAK_RPS, duration: `${STEP * 2}s` },
      ],
    },
    // Purchases run at the same time at a steady rate.
    checkout: {
      executor: 'constant-arrival-rate',
      exec: 'checkout',
      rate: ORDERS_PER_SECOND,
      timeUnit: '1s',
      duration: `${STEP * 6}s`,
      preAllocatedVUs: 20,
      maxVUs: 100,
    },
  },
  // Answers by status, listed in the summary to tell failures apart.
  thresholds: {
    'responses{status:200}': ['count>=0'],
    'responses{status:201}': ['count>=0'],
    'responses{status:409}': ['count>=0'],
    'responses{status:429}': ['count>=0'],
    'responses{status:5xx}': ['count>=0'],
    'responses{status:other}': ['count>=0'],
    'http_req_failed{scenario:browse}': ['rate<0.001'],
    'http_req_duration{scenario:browse}': ['p(95)<300'],
    'http_req_failed{scenario:checkout}': ['rate<0.01'],
    checkout_duration: ['p(95)<1500'],
  },
  summaryTrendStats: ['avg', 'med', 'p(90)', 'p(95)', 'p(99)', 'max'],
}

// Every iteration is a different visitor, so each one gets its own address.
function client() {
  let id = 0
  try {
    id = exec.scenario.iterationInTest + (exec.scenario.name === 'checkout' ? 60000 : 0)
  } catch {
    // setup() runs outside any scenario and uses the first address.
  }
  return `198.18.${Math.floor(id / 250) % 256}.${(id % 250) + 1}`
}

const storeOf = () => STORES[exec.vu.idInTest % STORES.length]

function headers(extra = {}, store = storeOf()) {
  return { Host: store, Origin: `http://${store}`, 'X-Forwarded-For': client(), 'Content-Type': 'application/json', ...extra }
}

const api = (path) => `${BASE}/api/v1${path}`

function record(response) {
  const status = [200, 201, 409, 429].includes(response.status) ? String(response.status) : response.status >= 500 ? '5xx' : 'other'
  responses.add(1, { status })
  return response
}

export function setup() {
  const stores = {}
  for (const store of STORES) {
    const products = http.get(api('/catalog/products?limit=50'), { headers: headers({}, store) }).json('items')
    if (!products || products.length === 0) fail(`${store} has no products; run scripts/load-test.sh`)
    const slugs = products.map((product) => product.slug)
    const stocked = http.get(api('/catalog/products/estoque-de-carga'), { headers: headers({}, store) }).json()
    stores[store] = { slugs, variant: stocked.variants[0].id }
  }
  return { stores, queries: ['caneca', 'camiseta', 'cafe', 'presente'] }
}

export function browse(data) {
  const { slugs } = data.stores[storeOf()]
  const slug = slugs[Math.floor(Math.random() * slugs.length)]
  const answers = http.batch([
    ['GET', api('/storefront/config'), null, { headers: headers(), tags: { name: 'config' } }],
    ['GET', api('/catalog/products'), null, { headers: headers(), tags: { name: 'listing' } }],
    ['GET', api(`/catalog/products/${slug}`), null, { headers: headers(), tags: { name: 'product' } }],
  ])
  answers.forEach(record)
  check(answers, { 'catalog answers 200': (all) => all.every((response) => response.status === 200) })
  if (Math.random() < 0.3) {
    const query = data.queries[Math.floor(Math.random() * data.queries.length)]
    const search = record(http.get(api(`/catalog/products?q=${query}`), { headers: headers(), tags: { name: 'search' } }))
    check(search, { 'search answers 200': (response) => response.status === 200 })
  }
}

export function checkout(data) {
  // The cart travels in an explicit header, so the cookie jar of the VU is not used.
  http.cookieJar().clear(BASE)
  const { variant } = data.stores[storeOf()]
  const started = Date.now()
  group('checkout', () => {
    const added = record(http.post(api('/cart/items'), JSON.stringify({ variantId: variant, quantity: 1 }), { headers: headers(), tags: { name: 'add_to_cart' } }))
    if (!check(added, { 'item added': (response) => response.status === 200 })) return
    const cookie = added.cookies.wl_cart?.[0]?.value
    const withCart = (extra = {}) => headers({ Cookie: `wl_cart=${cookie}`, ...extra })

    let summary = http.post(api('/checkout/summary'), JSON.stringify({ postalCode: '01310100' }), { headers: withCart(), tags: { name: 'summary' } })
    const method = summary.json('shippingOptions.0.id')
    summary = http.post(api('/checkout/summary'), JSON.stringify({ shippingMethodId: method, postalCode: '01310100' }), { headers: withCart(), tags: { name: 'summary' } })
    if (!check(summary, { 'summary priced': (response) => response.status === 200 && response.json('total') > 0 })) return

    const order = record(http.post(api('/checkout/orders'), JSON.stringify({
      customerName: 'Comprador de Carga', customerEmail: `carga+${exec.scenario.iterationInTest}@exemplo.test`,
      shippingMethodId: method, expectedTotal: summary.json('total'),
      address: { recipient: 'Comprador de Carga', postalCode: '01310100', street: 'Avenida Paulista', number: '1000', district: 'Bela Vista', city: 'São Paulo', state: 'SP' },
    }), { headers: withCart({ 'Idempotency-Key': `carga-${exec.scenario.iterationInTest}-${Date.now()}` }), tags: { name: 'place_order' } }))
    if (check(order, { 'order placed': (response) => response.status === 201 })) ordersPlaced.add(1)
  })
  checkoutDuration.add(Date.now() - started)
}
