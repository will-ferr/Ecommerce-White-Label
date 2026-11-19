import type { AuthStore } from '@whitelabel/auth'
import { inject, type InjectionKey } from 'vue'

export type PanelPortal = 'staff' | 'platform'

export interface PanelContext {
  portal: PanelPortal
  title: string
}

export const authKey: InjectionKey<AuthStore> = Symbol('auth')
export const panelKey: InjectionKey<PanelContext> = Symbol('panel')

export function useAuth(): AuthStore {
  const auth = inject(authKey)
  if (!auth) throw new Error('The auth store was not provided')
  return auth
}

export function usePanel(): PanelContext {
  const panel = inject(panelKey)
  if (!panel) throw new Error('The panel context was not provided')
  return panel
}
