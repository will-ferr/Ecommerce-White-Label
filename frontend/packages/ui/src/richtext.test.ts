import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import RichText from './components/RichText.vue'
import { parseRichText, safeHref } from './richtext'

describe('parseRichText', () => {
  it('reads headings, paragraphs, lists, bold text and links', () => {
    const blocks = parseRichText('## Trocas\n\nVocê tem **7 dias**\npara trocar.\n\n- Produto sem uso\n- Com [nota fiscal](/paginas/notas)')
    expect(blocks).toEqual([
      { type: 'heading', level: 2, content: [{ type: 'text', text: 'Trocas' }] },
      { type: 'paragraph', content: [{ type: 'text', text: 'Você tem ' }, { type: 'strong', text: '7 dias' }, { type: 'text', text: ' para trocar.' }] },
      {
        type: 'list',
        items: [
          [{ type: 'text', text: 'Produto sem uso' }],
          [{ type: 'text', text: 'Com ' }, { type: 'link', text: 'nota fiscal', href: '/paginas/notas' }],
        ],
      },
    ])
  })

  it('keeps unsafe links as plain text', () => {
    for (const href of ['javascript:alert(1)', 'http://inseguro.test', '//outro.test/x', 'data:text/html,oi']) {
      expect(safeHref(href)).toBeNull()
    }
    const [block] = parseRichText('[clique](javascript:alert(1))')
    expect(block?.type).toBe('paragraph')
    const content = block?.type === 'paragraph' ? block.content : []
    expect(content.some((part) => part.type === 'link')).toBe(false)
    expect(content.map((part) => part.text).join('')).toBe('[clique](javascript:alert(1))')
    expect(safeHref('https://loja.test/a')).toBe('https://loja.test/a')
  })
})

describe('RichText', () => {
  it('renders markup as text, never as HTML', () => {
    const wrapper = mount(RichText, { props: { source: '<script>alert(1)</script>\n\n<img src=x onerror=alert(1)>' } })
    expect(wrapper.find('script').exists()).toBe(false)
    expect(wrapper.find('img').exists()).toBe(false)
    expect(wrapper.text()).toContain('<script>alert(1)</script>')
  })

  it('renders headings and safe links', () => {
    const wrapper = mount(RichText, { props: { source: '## Fale conosco\n\nEscreva para [nosso time](https://loja.test/contato).' } })
    expect(wrapper.find('h2').text()).toBe('Fale conosco')
    expect(wrapper.find('a').attributes('href')).toBe('https://loja.test/contato')
    expect(wrapper.find('a').attributes('rel')).toContain('noopener')
  })
})
