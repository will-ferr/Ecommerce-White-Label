import type { Page, Sender } from './commerce'
import type { components } from './schema'

type ThemeTokens = components['schemas']['ThemeTokens']

/** Name and search settings a store team edits. Currency, locale and timezone are read-only. */
export interface StoreProfile {
  storeName: string
  seoTitle: string
  seoDescription: string
  seoIndexable: boolean
  currency: string
  locale: string
  timezone: string
}

export interface StoreTheme {
  version: number
  tokens: ThemeTokens
  versions: { version: number; status: 'published' | 'archived'; createdAt: string }[]
  choices: { fonts: string[]; radius: string[]; density: string[]; headers: string[] }
}

export interface Coupon {
  id?: string
  code: string
  description: string
  kind: 'percent' | 'fixed' | 'free_shipping'
  value: number
  minSubtotal: number
  startsAt: string | null
  endsAt: string | null
  usageLimit: number | null
  perCustomerLimit: number | null
  active: boolean
  usedCount?: number
}

export interface ContentPage {
  id: string
  slug: string
  title: string
  body: string
  seoDescription: string
  status: 'draft' | 'published'
  showInFooter: boolean
  version: number
  updatedAt: string
}

export type ContentPageInput = Omit<ContentPage, 'id' | 'updatedAt'>

export interface SalesReport {
  from: string
  to: string
  timezone: string
  currency: string
  paidOrders: number
  revenue: number
  averageTicket: number
  discounts: number
  canceledOrders: number
  allOrders: number
  days: { date: string; orders: number; revenue: number }[]
  topProducts: { name: string; quantity: number; revenue: number }[]
}

export interface CustomerSummary {
  id: string
  name: string
  email: string
  status: string
  emailVerified: boolean
  createdAt: string
}

const query = (params: Record<string, string | undefined>) => {
  const search = new URLSearchParams()
  for (const [key, value] of Object.entries(params)) if (value) search.set(key, value)
  const text = search.toString()
  return text ? `?${text}` : ''
}

const segment = (value: string) => encodeURIComponent(value)

/** Store management endpoints. Paths are relative to /api/v1/staff. */
export function createStoreManagementClient(send: Sender) {
  const items = async <T>(path: string) => (await send<{ items: T[] }>('GET', path)).items
  return {
    profile: () => send<StoreProfile>('GET', '/store/settings'),
    saveProfile: (input: Pick<StoreProfile, 'storeName' | 'seoTitle' | 'seoDescription' | 'seoIndexable'>) => send<void>('PUT', '/store/settings', input),
    theme: () => send<StoreTheme>('GET', '/store/theme'),
    publishTheme: (tokens: ThemeTokens) => send<{ version: number }>('POST', '/store/theme', { tokens }),
    rollbackTheme: (version: number) => send<void>('POST', '/store/theme/rollback', { version }),
    coupons: () => items<Coupon>('/promotions/coupons'),
    saveCoupon: (id: string | undefined, input: Coupon) =>
      id ? send<{ id: string }>('PUT', `/promotions/coupons/${segment(id)}`, input) : send<{ id: string }>('POST', '/promotions/coupons', input),
    pages: () => items<ContentPage>('/content/pages'),
    savePage: (id: string | undefined, input: ContentPageInput) =>
      id ? send<{ id: string }>('PUT', `/content/pages/${segment(id)}`, input) : send<{ id: string }>('POST', '/content/pages', input),
    deletePage: (id: string) => send<void>('DELETE', `/content/pages/${segment(id)}`),
    salesReport: (from?: string, to?: string) => send<SalesReport>('GET', `/reports/sales${query({ from, to })}`),
    customers: (params: { q?: string; cursor?: string } = {}) => send<Page<CustomerSummary>>('GET', `/customers${query(params)}`),
  }
}

export interface StoreSummary {
  slug: string
  name: string
  status: 'active' | 'suspended'
  plan: string
  createdAt: string
}

export interface StoreDetail extends StoreSummary {
  currency: string
  indexable: boolean
  domains: { host: string; kind: 'subdomain' | 'custom'; verifiedAt: string | null; verificationName?: string; verificationValue?: string }[]
  features: { key: string; inPlan: boolean; enabled: boolean }[]
}

/** Platform console endpoints. Paths are relative to /api/v1/platform. */
export function createConsoleClient(send: Sender) {
  return {
    plans: async () => (await send<{ items: { code: string; name: string }[] }>('GET', '/plans')).items,
    stores: () => send<{ items: StoreSummary[]; baseDomain: string }>('GET', '/stores'),
    createStore: (input: { slug: string; name: string; plan: string }) => send<{ slug: string; host: string }>('POST', '/stores', input),
    store: (slug: string) => send<StoreDetail>('GET', `/stores/${segment(slug)}`),
    setStatus: (slug: string, status: 'active' | 'suspended') => send<void>('POST', `/stores/${segment(slug)}/status`, { status }),
    addDomain: (slug: string, host: string) =>
      send<{ verificationName: string; verificationValue: string }>('POST', `/stores/${segment(slug)}/domains`, { host }),
    verifyDomain: (slug: string, host: string) => send<void>('POST', `/stores/${segment(slug)}/domains/${segment(host)}/verify`, {}),
    removeDomain: (slug: string, host: string) => send<void>('DELETE', `/stores/${segment(slug)}/domains/${segment(host)}`),
    setFeature: (slug: string, key: string, enabled: boolean) => send<void>('PUT', `/stores/${segment(slug)}/features/${segment(key)}`, { enabled }),
    invite: (slug: string, input: { name: string; email: string; role?: string }) => send<void>('POST', `/stores/${segment(slug)}/invitations`, input),
  }
}

export type StoreManagementClient = ReturnType<typeof createStoreManagementClient>
export type ConsoleClient = ReturnType<typeof createConsoleClient>
