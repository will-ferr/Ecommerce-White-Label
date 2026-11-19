<script setup lang="ts">
import { computed, h, type VNode } from 'vue'

import { parseRichText, type Inline } from '../richtext'

const props = defineProps<{ source: string }>()

const blocks = computed(() => parseRichText(props.source))

function inline(content: Inline[]): (VNode | string)[] {
  return content.map((part) => {
    if (part.type === 'strong') return h('strong', part.text)
    if (part.type === 'link') {
      const external = part.href.startsWith('https://')
      return h('a', {
        href: part.href, class: 'font-semibold text-primary underline underline-offset-4',
        ...(external ? { rel: 'noopener noreferrer nofollow', target: '_blank' } : {}),
      }, part.text)
    }
    return part.text
  })
}

const Content = () =>
  blocks.value.map((block) => {
    switch (block.type) {
      case 'heading':
        return h(block.level === 2 ? 'h2' : 'h3', { class: block.level === 2 ? 'font-display text-2xl text-ink' : 'text-lg font-semibold text-ink' }, inline(block.content))
      case 'list':
        return h('ul', { class: 'list-disc space-y-1 pl-6' }, block.items.map((item) => h('li', inline(item))))
      default:
        return h('p', inline(block.content))
    }
  })
</script>

<template>
  <div class="max-w-prose space-y-4 leading-relaxed text-ink">
    <Content />
  </div>
</template>
