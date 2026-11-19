import { createPanelApp } from '@whitelabel/panel-shell'

import './main.css'

createPanelApp({
  portal: 'platform',
  title: 'Console da plataforma',
  heading: 'Operação da plataforma',
  description: 'Crie e acompanhe lojas, domínios, recursos do plano e a equipe da plataforma.',
  navigation: [{ to: '/lojas', label: 'Lojas', permission: 'stores:read' }],
  routes: [
    { path: '/lojas', component: () => import('./views/StoresView.vue'), meta: { permission: 'stores:read' } },
    { path: '/lojas/:slug', component: () => import('./views/StoreView.vue'), props: true, meta: { permission: 'stores:read' } },
  ],
}).mount('#app')
