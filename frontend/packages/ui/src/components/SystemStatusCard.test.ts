import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import SystemStatusCard from './SystemStatusCard.vue'

describe('SystemStatusCard', () => {
  it('announces the loading state', () => {
    const wrapper = mount(SystemStatusCard, { props: { state: 'loading' } })

    expect(wrapper.find('section').attributes('aria-busy')).toBe('true')
    expect(wrapper.find('[role="status"]').text()).toContain('Consultando')
  })

  it.each([
    ['ok', 'Operacional'],
    ['degraded', 'Degradado'],
    ['unavailable', 'Indisponível'],
  ] as const)('shows the %s state', (state, label) => {
    const wrapper = mount(SystemStatusCard, { props: { state, version: '1.2.3' } })

    expect(wrapper.text()).toContain(label)
    expect(wrapper.text()).toContain('Versão 1.2.3')
  })

  it('shows the error with a support code and offers a retry', async () => {
    const wrapper = mount(SystemStatusCard, {
      props: { state: 'error', errorMessage: 'Sem conexão.', correlationId: 'corr-1' },
    })

    expect(wrapper.find('[role="alert"]').text()).toContain('Sem conexão.')
    expect(wrapper.text()).toContain('corr-1')

    await wrapper.find('button').trigger('click')

    expect(wrapper.emitted('retry')).toHaveLength(1)
  })
})
