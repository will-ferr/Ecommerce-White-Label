import tailwindcss from '@tailwindcss/vite'

export default defineNuxtConfig({
  compatibilityDate: '2026-11-01',
  modules: ['@pinia/nuxt'],
  css: ['~/assets/main.css'],
  devtools: { enabled: false },
  telemetry: false,
  build: { transpile: ['@whitelabel/ui', '@whitelabel/api-client'] },
  vite: { plugins: [tailwindcss()] },
  app: {
    head: {
      htmlAttrs: { lang: 'pt-BR' },
      link: [{ rel: 'icon', href: 'data:,' }],
    },
  },
  runtimeConfig: {
    // Overridden by NUXT_API_INTERNAL_URL. Used only for server-side rendering.
    apiInternalUrl: 'http://localhost:8080',
  },
  nitro: {
    devProxy: { '/api': { target: 'http://localhost:8080/api', changeOrigin: true } },
  },
})
