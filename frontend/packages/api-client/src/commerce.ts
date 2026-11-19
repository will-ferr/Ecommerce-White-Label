// Types and clients of the catalog, cart, checkout and order endpoints.

export interface Image {
  url: string
  alt: string
}

export interface Category {
  id: string
  parentId: string | null
  name: string
  slug: string
  description: string
  position: number
}

export interface ProductCard {
  id: string
  name: string
  slug: string
  brand: string
  image: Image | null
  price: number
  compareAtPrice: number | null
  hasPriceRange: boolean
  available: boolean
}

export interface StoreVariant {
  id: string
  label: string
  options: Record<string, string>
  price: number
  compareAtPrice: number | null
  available: boolean
}

export interface ProductDetail {
  id: string
  name: string
  slug: string
  brand: string
  description: string
  seoTitle: string
  seoDescription: string
  category: Category | null
  images: Image[]
  variants: StoreVariant[]
}

export interface Page<T> {
  items: T[]
  nextCursor?: string
}

export interface CartLine {
  variantId: string
  productName: string
  productSlug: string
  label: string
  sku: string
  quantity: number
  unitPrice: number
  total: number
  available: boolean
}

export interface Cart {
  lines: CartLine[]
  itemCount: number
  subtotal: number
  currency: string
  hasUnavailable: boolean
}

export interface ShippingOption {
  id: string
  name: string
  kind: 'pickup' | 'flat' | 'table'
  price: number
  minDays: number
  maxDays: number
}

export interface CheckoutSummary {
  cart: Cart
  shippingOptions: ShippingOption[]
  shipping: ShippingOption | null
  subtotal: number
  shippingAmount: number
  tax: number
  total: number
  currency: string
  paymentLabel: string
  /** The store uses the test provider: nothing is charged. */
  testPayment: boolean
}

export interface Address {
  recipient: string
  postalCode: string
  street: string
  number: string
  complement: string
  district: string
  city: string
  state: string
}

export interface CheckoutInput {
  customerName: string
  customerEmail: string
  address: Address
  shippingMethodId: string
  expectedTotal: number
}

export interface PlacedOrder {
  orderNumber: number
  status: string
  total: number
  currency: string
  orderToken: string
  paymentLabel: string
}

export interface OrderItem {
  variantId: string
  productName: string
  label: string
  sku: string
  quantity: number
  unitPrice: number
  total: number
}

export interface Order {
  id: string
  number: number
  status: string
  customerName: string
  customerEmail: string
  shippingAddress: Address
  shippingMethod: string
  subtotal: number
  shippingAmount: number
  discount: number
  tax: number
  total: number
  currency: string
  trackingCode: string
  cancelReason: string
  paymentDueAt: string
  createdAt: string
  items: OrderItem[]
  payment?: { provider: string; label: string; status: string; amount: number }
  history?: { from: string; to: string; actorType: string; note: string; createdAt: string }[]
  notes?: { authorId: string; body: string; createdAt: string }[]
  nextStatuses?: string[]
  testPayment: boolean
}

export interface SavedAddress extends Address {
  id: string
  label: string
  isDefault: boolean
}

export interface Stock {
  onHand: number
  reserved: number
  available: number
  minQuantity: number
  low: boolean
}

export interface AdminVariant {
  id?: string
  sku: string
  gtin: string
  options: Record<string, string>
  label?: string
  weightGrams: number
  price: number
  compareAtPrice: number | null
  costPrice: number | null
  status?: string
  stock?: Stock
}

export interface AdminProduct {
  id: string
  categoryId: string | null
  name: string
  slug: string
  description: string
  brand: string
  status: 'draft' | 'active' | 'archived'
  featured: boolean
  seoTitle: string
  seoDescription: string
  version: number
  images: Image[]
  variants: AdminVariant[]
}

export type ProductInput = Omit<AdminProduct, 'id' | 'version'> & { version?: number }

/** One line of a shipping table: the price for parcels up to a weight sent to a range of CEPs. */
export interface ShippingRate {
  postalCodeStart: string
  postalCodeEnd: string
  maxWeightGrams: number
  price: number
  minDays: number
  maxDays: number
}

export interface ShippingMethod extends ShippingOption {
  active: boolean
  rates: ShippingRate[]
}

/** Sends one request and returns the decoded answer. */
export type Sender = <T>(method: string, path: string, body?: unknown, headers?: Record<string, string>) => Promise<T>

const query = (params: Record<string, string | number | boolean | undefined>) => {
  const search = new URLSearchParams()
  for (const [key, value] of Object.entries(params)) {
    if (value !== undefined && value !== '' && value !== false) search.set(key, String(value))
  }
  const text = search.toString()
  return text ? `?${text}` : ''
}

const segment = (value: string | number) => encodeURIComponent(String(value))

/** Store endpoints used by buyers. Paths are relative to /api/v1. */
export function createStoreClient(send: Sender) {
  const items = async <T>(path: string) => (await send<{ items: T[] }>('GET', path)).items
  return {
    categories: () => items<Category>('/catalog/categories'),
    products: (params: { category?: string; q?: string; featured?: boolean; cursor?: string; limit?: number } = {}) =>
      send<Page<ProductCard>>('GET', `/catalog/products${query(params)}`),
    product: (slug: string) => send<ProductDetail>('GET', `/catalog/products/${segment(slug)}`),
    cart: () => send<Cart>('GET', '/cart'),
    addItem: (variantId: string, quantity: number) => send<Cart>('POST', '/cart/items', { variantId, quantity }),
    setItem: (variantId: string, quantity: number) => send<Cart>('PUT', `/cart/items/${segment(variantId)}`, { quantity }),
    removeItem: (variantId: string) => send<Cart>('DELETE', `/cart/items/${segment(variantId)}`),
    mergeCart: () => send<Cart>('POST', '/cart/merge', {}),
    summary: (shippingMethodId?: string, postalCode?: string) =>
      send<CheckoutSummary>('POST', '/checkout/summary', { shippingMethodId: shippingMethodId ?? null, postalCode: postalCode ?? '' }),
    placeOrder: (input: CheckoutInput, idempotencyKey: string) =>
      send<PlacedOrder>('POST', '/checkout/orders', input, { 'Idempotency-Key': idempotencyKey }),
    order: (number: number, token?: string) => send<Order>('GET', `/orders/${segment(number)}${query({ token })}`),
    testPayment: (number: number, token: string | undefined, approved: boolean) =>
      send<Order>('POST', `/orders/${segment(number)}/test-payment`, { token: token ?? '', approved }),
    accountOrders: (cursor?: string) => send<Page<Order>>('GET', `/account/orders${query({ cursor })}`),
    addresses: () => items<SavedAddress>('/account/addresses'),
    addAddress: (input: Address & { label: string; isDefault: boolean }) => send<{ id: string }>('POST', '/account/addresses', input),
    deleteAddress: (id: string) => send<void>('DELETE', `/account/addresses/${segment(id)}`),
  }
}

/** Management endpoints used by the store team. Paths are relative to /api/v1/staff. */
export function createCommerceAdminClient(send: Sender) {
  const items = async <T>(path: string) => (await send<{ items: T[] }>('GET', path)).items
  return {
    categories: () => items<Category>('/catalog/categories'),
    createCategory: (input: { name: string; slug?: string; parentId?: string | null; position?: number }) =>
      send<{ id: string }>('POST', '/catalog/categories', input),
    updateCategory: (id: string, input: { name: string; slug: string; parentId: string | null; position: number; description: string }) =>
      send<void>('PUT', `/catalog/categories/${segment(id)}`, input),
    deleteCategory: (id: string) => send<void>('DELETE', `/catalog/categories/${segment(id)}`),
    products: (params: { q?: string; status?: string; cursor?: string } = {}) => send<Page<AdminProduct>>('GET', `/catalog/products${query(params)}`),
    product: (id: string) => send<AdminProduct>('GET', `/catalog/products/${segment(id)}`),
    createProduct: (input: ProductInput) => send<{ id: string }>('POST', '/catalog/products', input),
    updateProduct: (id: string, input: ProductInput) => send<void>('PUT', `/catalog/products/${segment(id)}`, input),
    adjustStock: (input: { variantId: string; delta: number; reason: string; minQuantity?: number }) =>
      send<Stock>('POST', '/inventory/adjustments', input),
    orders: (params: { status?: string; q?: string; cursor?: string } = {}) => send<Page<Order>>('GET', `/orders${query(params)}`),
    order: (id: string) => send<Order>('GET', `/orders/${segment(id)}`),
    changeStatus: (id: string, input: { status: string; trackingCode?: string; note?: string }) =>
      send<void>('POST', `/orders/${segment(id)}/status`, input),
    addNote: (id: string, body: string) => send<void>('POST', `/orders/${segment(id)}/notes`, { body }),
    shippingMethods: () => items<ShippingMethod>('/shipping/methods'),
    saveShippingMethod: (id: string | undefined, input: Omit<ShippingMethod, 'id'>) =>
      id ? send<{ id: string }>('PUT', `/shipping/methods/${segment(id)}`, input) : send<{ id: string }>('POST', '/shipping/methods', input),
  }
}

export type StoreClient = ReturnType<typeof createStoreClient>
export type CommerceAdminClient = ReturnType<typeof createCommerceAdminClient>
