import { createPanelApp } from '@whitelabel/panel-shell'

import './main.css'

createPanelApp({
  portal: 'staff',
  title: 'Admin da loja',
  heading: 'Painel da loja',
  description: 'A gestão de produtos, pedidos e aparência da loja será adicionada nas próximas fases.',
}).mount('#app')
