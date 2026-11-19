import type { ThemeTokens } from '@whitelabel/api-client'

const HEX_COLOR = /^#[0-9a-f]{6}$/

const COLOR_VARIABLES: Record<keyof ThemeTokens['colors'], string> = {
  canvas: '--wl-color-canvas',
  surface: '--wl-color-surface',
  ink: '--wl-color-ink',
  muted: '--wl-color-muted',
  line: '--wl-color-line',
  primary: '--wl-color-primary',
  primaryStrong: '--wl-color-primary-strong',
  onPrimary: '--wl-color-on-primary',
}

// System font stacks: stores load no external font and expose no visitor data.
const FONT_STACKS: Record<string, string> = {
  system: "ui-sans-serif,system-ui,-apple-system,'Segoe UI',Roboto,Arial,sans-serif",
  humanist: "Seravek,'Gill Sans Nova',Ubuntu,Calibri,'DejaVu Sans',sans-serif",
  geometric: "Avenir,Montserrat,Corbel,'URW Gothic',source-sans-pro,sans-serif",
  serif: "Charter,'Bitstream Charter','Sitka Text',Cambria,Georgia,serif",
  slab: "Rockwell,'Rockwell Nova','Roboto Slab','DejaVu Serif','Sitka Small',serif",
}

const RADIUS: Record<string, [control: string, card: string]> = {
  none: ['0', '0'],
  small: ['0.25rem', '0.375rem'],
  medium: ['0.5rem', '0.875rem'],
  large: ['0.875rem', '1.5rem'],
}

const SECTION_SPACING: Record<string, string> = {
  compact: '2rem',
  comfortable: '3rem',
  spacious: '5rem',
}

/**
 * Builds the CSS of a store theme. Only values from fixed lists and strict color
 * formats reach the output, even if the API returns something unexpected.
 */
export function themeToCss(tokens: ThemeTokens): string {
  const declarations: string[] = []

  for (const [token, variable] of Object.entries(COLOR_VARIABLES)) {
    const value = tokens.colors[token as keyof ThemeTokens['colors']]
    if (HEX_COLOR.test(value)) declarations.push(`${variable}:${value}`)
  }

  const body = FONT_STACKS[tokens.typography.body]
  if (body) declarations.push(`--wl-font-sans:${body}`)
  const display = FONT_STACKS[tokens.typography.display]
  if (display) declarations.push(`--wl-font-display:${display}`)

  const radius = RADIUS[tokens.shape.radius]
  if (radius) declarations.push(`--wl-radius-control:${radius[0]}`, `--wl-radius-card:${radius[1]}`)

  const spacing = SECTION_SPACING[tokens.spacing.density]
  if (spacing) declarations.push(`--wl-space-section:${spacing}`)

  // The stylesheet with the default tokens loads after this rule, so the theme
  // needs a more specific selector than its :root to take effect.
  return `html:root{${declarations.join(';')}}`
}

/** Returns the address only when it is an absolute HTTPS URL. */
export function safeAssetUrl(value: string): string | undefined {
  if (!value) return undefined
  try {
    const url = new URL(value)
    return url.protocol === 'https:' && !url.username && !url.password ? url.href : undefined
  } catch {
    return undefined
  }
}
