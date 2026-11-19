<script setup lang="ts">
defineProps<{ name: string }>()

const api = useStoreApi()
// The footer is secondary: if its links fail to load, the page still renders.
const { data: pages } = await useAsyncData('footer-pages', () => api.footerPages().catch(() => []), { default: () => [] })
</script>

<template>
  <footer class="border-t border-line bg-surface">
    <div class="mx-auto flex max-w-6xl flex-wrap items-center justify-between gap-4 px-4 py-6 text-sm text-muted sm:px-6">
      <p>{{ name }}</p>
      <nav v-if="pages.length" aria-label="Institucional">
        <ul class="flex flex-wrap gap-x-5 gap-y-2">
          <li v-for="page in pages" :key="page.slug">
            <NuxtLink :to="`/paginas/${page.slug}`" class="inline-flex min-h-11 items-center underline underline-offset-4">{{ page.title }}</NuxtLink>
          </li>
        </ul>
      </nav>
    </div>
  </footer>
</template>
