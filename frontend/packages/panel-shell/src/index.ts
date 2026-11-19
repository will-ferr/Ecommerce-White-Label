import { createPortalClient } from '@whitelabel/api-client'
import { createAuthStore } from '@whitelabel/auth'
import { createPinia } from 'pinia'
import { createApp, h } from 'vue'
import { createRouter, createWebHistory, type RouteRecordRaw } from 'vue-router'

import { authKey, panelKey, type PanelPortal } from './context'
import PanelLayout from './PanelLayout.vue'
import { useErrorStore } from './stores/errors'

export interface PanelOptions {
  title: string
  heading: string
  description: string
  portal: PanelPortal
  routes?: RouteRecordRaw[]
}

const PORTAL_BASE = { staff: '/api/v1/staff', platform: '/api/v1/platform' } as const

/** Accepts only addresses inside this panel after signing in. */
export function safeRedirect(target: unknown): string {
  return typeof target === 'string' && target.startsWith('/') && !target.startsWith('//') && !target.includes('\\') ? target : '/'
}

/** Creates a panel with routing, session and central error handling configured. */
export function createPanelApp({ title, heading, description, portal, routes = [] }: PanelOptions) {
  const app = createApp({ render: () => h(PanelLayout, { title }) })
  const pinia = createPinia()
  app.use(pinia)

  const auth = createAuthStore(createPortalClient(PORTAL_BASE[portal]))(pinia)
  app.provide(authKey, auth)
  app.provide(panelKey, { portal, title })

  const router = createRouter({
    history: createWebHistory(),
    routes: [
      { path: '/entrar', name: 'login', component: () => import('./views/LoginView.vue'), meta: { public: true } },
      { path: '/recuperar-senha', name: 'forgot', component: () => import('./views/ForgotView.vue'), meta: { public: true } },
      { path: '/redefinir-senha', name: 'reset', component: () => import('./views/TokenView.vue'), props: { mode: 'reset' }, meta: { public: true } },
      { path: '/convite', name: 'invitation', component: () => import('./views/TokenView.vue'), props: { mode: 'invitation' }, meta: { public: true } },
      { path: '/', name: 'status', component: () => import('./views/StatusView.vue'), props: { heading, description } },
      { path: '/equipe', name: 'team', component: () => import('./views/TeamView.vue'), meta: { permission: 'team:read' } },
      { path: '/auditoria', name: 'audit', component: () => import('./views/AuditView.vue'), meta: { permission: 'audit:read' } },
      { path: '/seguranca', name: 'security', component: () => import('./views/SecurityView.vue') },
      ...routes,
      { path: '/:pathMatch(.*)*', name: 'not-found', component: () => import('./views/NotFoundView.vue'), meta: { public: true } },
    ],
  })

  router.beforeEach(async (to) => {
    await auth.restore()
    if (to.meta.public) {
      return to.name === 'login' && auth.isAuthenticated ? { path: '/' } : true
    }
    if (!auth.isAuthenticated) return { name: 'login', query: { redirect: to.fullPath } }
    const permission = to.meta.permission
    return typeof permission === 'string' && !auth.can(permission) ? { path: '/' } : true
  })

  app.use(router)

  const errors = useErrorStore(pinia)
  app.config.errorHandler = (error) => errors.report(error)
  router.onError((error) => errors.report(error))
  window.addEventListener('unhandledrejection', (event) => errors.report(event.reason))

  document.title = title
  return app
}
