import { createPanelApp } from '@whitelabel/panel-shell'

import './main.css'

createPanelApp({
  portal: 'staff',
  title: 'Admin da loja',
  heading: 'Painel da loja',
  description: 'Gerencie produtos, estoque, pedidos e a equipe da sua loja.',
  navigation: [
    { to: '/produtos', label: 'Produtos', permission: 'catalog:read' },
    { to: '/categorias', label: 'Categorias', permission: 'catalog:read' },
    { to: '/pedidos', label: 'Pedidos', permission: 'orders:read' },
    { to: '/frete', label: 'Frete', permission: 'orders:read' },
  ],
  routes: [
    { path: '/produtos', component: () => import('./views/ProductsView.vue'), meta: { permission: 'catalog:read' } },
    { path: '/produtos/novo', component: () => import('./views/ProductEditView.vue'), meta: { permission: 'catalog:manage' } },
    { path: '/produtos/:id', component: () => import('./views/ProductEditView.vue'), props: true, meta: { permission: 'catalog:read' } },
    { path: '/categorias', component: () => import('./views/CategoriesView.vue'), meta: { permission: 'catalog:read' } },
    { path: '/pedidos', component: () => import('./views/OrdersView.vue'), meta: { permission: 'orders:read' } },
    { path: '/pedidos/:id', component: () => import('./views/OrderView.vue'), props: true, meta: { permission: 'orders:read' } },
    { path: '/frete', component: () => import('./views/ShippingView.vue'), meta: { permission: 'orders:read' } },
  ],
}).mount('#app')
