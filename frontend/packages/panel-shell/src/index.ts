import { createPinia } from 'pinia'
import { createApp, h } from 'vue'
import { createRouter, createWebHistory, type RouteRecordRaw } from 'vue-router'

import PanelLayout from './PanelLayout.vue'
import { useErrorStore } from './stores/errors'

export interface PanelOptions {
  title: string
  heading: string
  description: string
  routes?: RouteRecordRaw[]
}

/** Creates a panel with routing, state and central error handling configured. */
export function createPanelApp({ title, heading, description, routes = [] }: PanelOptions) {
  const app = createApp({ render: () => h(PanelLayout, { title }) })
  const pinia = createPinia()
  const router = createRouter({
    history: createWebHistory(),
    routes: [
      {
        path: '/',
        name: 'status',
        component: () => import('./views/StatusView.vue'),
        props: { heading, description },
      },
      ...routes,
      {
        path: '/:pathMatch(.*)*',
        name: 'not-found',
        component: () => import('./views/NotFoundView.vue'),
      },
    ],
  })

  app.use(pinia)
  app.use(router)

  const errors = useErrorStore(pinia)
  app.config.errorHandler = (error) => errors.report(error)
  router.onError((error) => errors.report(error))
  window.addEventListener('unhandledrejection', (event) => errors.report(event.reason))

  document.title = title
  return app
}
