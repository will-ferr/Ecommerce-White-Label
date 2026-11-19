import { createPanelApp } from '@whitelabel/panel-shell'

import './main.css'

createPanelApp({
  portal: 'platform',
  title: 'Console da plataforma',
  heading: 'Operação da plataforma',
  description: 'A gestão de lojas, planos e auditoria global será adicionada nas próximas fases.',
}).mount('#app')
