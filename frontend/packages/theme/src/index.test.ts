import type { ThemeTokens } from '@whitelabel/api-client'
import { describe, expect, it } from 'vitest'

import { safeAssetUrl, themeToCss } from './index'

function tokens(): ThemeTokens {
  return {
    colors: {
      canvas: '#f6f4ee',
      surface: '#fffefb',
      ink: '#18211f',
      muted: '#58635f',
      line: '#dcd8cc',
      primary: '#8a1c5c',
      primaryStrong: '#6d1448',
      onPrimary: '#ffffff',
    },
    typography: { body: 'humanist', display: 'serif' },
    shape: { radius: 'large' },
    spacing: { density: 'spacious' },
    layout: { header: 'centered' },
    assets: { logoUrl: '', faviconUrl: '' },
  }
}

describe('themeToCss', () => {
  it('maps every token to a CSS variable', () => {
    const css = themeToCss(tokens())

    expect(css).toContain('--wl-color-primary:#8a1c5c')
    expect(css).toContain('--wl-color-primary-strong:#6d1448')
    expect(css).toContain('--wl-radius-card:1.5rem')
    expect(css).toContain('--wl-space-section:5rem')
    expect(css).toContain('--wl-font-display:Charter')
    expect(css.startsWith('html:root{')).toBe(true)
  })

  it.each([
    'red',
    '#FFF',
    '#8a1c5c;background:url(https://evil.test)',
    '#8a1c5c}body{display:none}',
    '</style><script>alert(1)</script>',
    'var(--x)',
  ])('drops the unsafe color %s', (attack) => {
    const theme = tokens()
    theme.colors.primary = attack

    const css = themeToCss(theme)

    expect(css).not.toContain(attack)
    expect(css).not.toContain('--wl-color-primary:')
  })

  it('ignores values outside the allowed lists', () => {
    const theme = tokens() as unknown as {
      typography: { body: string }
      shape: { radius: string }
      spacing: { density: string }
    }
    theme.typography.body = "x;}</style><script>alert(1)</script>"
    theme.shape.radius = '999px;color:red'
    theme.spacing.density = 'url(https://evil.test)'

    const css = themeToCss(theme as unknown as ThemeTokens)

    expect(css).not.toContain('script')
    expect(css).not.toContain('999px')
    expect(css).not.toContain('evil.test')
  })

  it('keeps the rule structure intact', () => {
    const css = themeToCss(tokens())

    expect(css.match(/[{}]/g)).toEqual(['{', '}'])
  })
})

describe('safeAssetUrl', () => {
  it('accepts absolute HTTPS addresses', () => {
    expect(safeAssetUrl('https://cdn.example.com/logo.png')).toBe('https://cdn.example.com/logo.png')
  })

  it.each([
    '',
    'http://cdn.example.com/logo.png',
    'javascript:alert(1)',
    'data:image/svg+xml,<svg onload=alert(1)>',
    '//cdn.example.com/logo.png',
    '/logo.png',
    'https://user:pass@cdn.example.com/logo.png',
  ])('refuses %s', (value) => {
    expect(safeAssetUrl(value)).toBeUndefined()
  })
})
