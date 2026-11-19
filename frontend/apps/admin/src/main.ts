import { createPanelApp } from '@whitelabel/panel-shell'

import './main.css'

createPanelApp({
  portal: 'staff',
  title: 'Admin da loja',
  heading: 'Painel da loja',
  description: 'Gerencie produtos, estoque, pedidos, promoções, conteúdo e a equipe da sua loja.',
  navigation: [
    { to: '/relatorios', label: 'Relatórios', permission: 'reports:read' },
    { to: '/pedidos', label: 'Pedidos', permission: 'orders:read' },
    { to: '/produtos', label: 'Produtos', permission: 'catalog:read' },
    { to: '/categorias', label: 'Categorias', permission: 'catalog:read' },
    { to: '/clientes', label: 'Clientes', permission: 'customers:read' },
    { to: '/cupons', label: 'Cupons', permission: 'promotions:manage' },
    { to: '/frete', label: 'Frete', permission: 'orders:read' },
    { to: '/paginas', label: 'Páginas', permission: 'content:manage' },
    { to: '/aparencia', label: 'Aparência', permission: 'store:manage' },
    { to: '/loja', label: 'Loja', permission: 'store:manage' },
  ],
  routes: [
    { path: '/relatorios', component: () => import('./views/ReportsView.vue'), meta: { permission: 'reports:read' } },
    { path: '/produtos', component: () => import('./views/ProductsView.vue'), meta: { permission: 'catalog:read' } },
    { path: '/produtos/novo', component: () => import('./views/ProductEditView.vue'), meta: { permission: 'catalog:manage' } },
    { path: '/produtos/:id', component: () => import('./views/ProductEditView.vue'), props: true, meta: { permission: 'catalog:read' } },
    { path: '/categorias', component: () => import('./views/CategoriesView.vue'), meta: { permission: 'catalog:read' } },
    { path: '/pedidos', component: () => import('./views/OrdersView.vue'), meta: { permission: 'orders:read' } },
    { path: '/pedidos/:id', component: () => import('./views/OrderView.vue'), props: true, meta: { permission: 'orders:read' } },
    { path: '/clientes', component: () => import('./views/CustomersView.vue'), meta: { permission: 'customers:read' } },
    { path: '/cupons', component: () => import('./views/CouponsView.vue'), meta: { permission: 'promotions:manage' } },
    { path: '/frete', component: () => import('./views/ShippingView.vue'), meta: { permission: 'orders:read' } },
    { path: '/paginas', component: () => import('./views/PagesView.vue'), meta: { permission: 'content:manage' } },
    { path: '/aparencia', component: () => import('./views/ThemeView.vue'), meta: { permission: 'store:manage' } },
    { path: '/loja', component: () => import('./views/StoreSettingsView.vue'), meta: { permission: 'store:manage' } },
  ],
}).mount('#app')
