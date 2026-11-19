/**
 * A small text format for store pages. It is parsed into a tree that Vue
 * renders as elements, never as HTML, so a page cannot carry markup or scripts.
 *
 *   ## Title            heading, also ### for a smaller one
 *   - item              list item
 *   **bold**            strong text
 *   [text](https://…)   link to an HTTPS address or to a path of the store
 *   blank line          new paragraph
 */

export type Inline =
  | { type: 'text'; text: string }
  | { type: 'strong'; text: string }
  | { type: 'link'; text: string; href: string }

export type Block =
  | { type: 'heading'; level: 2 | 3; content: Inline[] }
  | { type: 'paragraph'; content: Inline[] }
  | { type: 'list'; items: Inline[][] }

const INLINE = /\*\*([^*]+)\*\*|\[([^\]]+)\]\(([^)\s]+)\)/g

/** Accepts only HTTPS addresses and paths inside the store. */
export function safeHref(href: string): string | null {
  if (href.startsWith('/') && !href.startsWith('//') && !href.includes('\\')) return href
  try {
    const url = new URL(href)
    return url.protocol === 'https:' && !url.username && !url.password ? url.href : null
  } catch {
    return null
  }
}

export function parseInline(text: string): Inline[] {
  const result: Inline[] = []
  let last = 0
  for (const match of text.matchAll(INLINE)) {
    if (match.index > last) result.push({ type: 'text', text: text.slice(last, match.index) })
    const [whole, bold, label, href] = match
    if (bold !== undefined) {
      result.push({ type: 'strong', text: bold })
    } else {
      const safe = safeHref(href ?? '')
      result.push(safe ? { type: 'link', text: label ?? '', href: safe } : { type: 'text', text: whole })
    }
    last = match.index + whole.length
  }
  if (last < text.length) result.push({ type: 'text', text: text.slice(last) })
  return result
}

export function parseRichText(source: string): Block[] {
  const blocks: Block[] = []
  for (const chunk of source.replace(/\r\n/g, '\n').split(/\n\s*\n/)) {
    const lines = chunk.split('\n').map((line) => line.trim()).filter(Boolean)
    if (!lines.length) continue
    const first = lines[0] as string
    if (lines.length === 1 && /^#{2,3} /.test(first)) {
      const level = first.startsWith('### ') ? 3 : 2
      blocks.push({ type: 'heading', level, content: parseInline(first.slice(level + 1)) })
    } else if (lines.every((line) => line.startsWith('- '))) {
      blocks.push({ type: 'list', items: lines.map((line) => parseInline(line.slice(2))) })
    } else {
      blocks.push({ type: 'paragraph', content: parseInline(lines.join(' ')) })
    }
  }
  return blocks
}
