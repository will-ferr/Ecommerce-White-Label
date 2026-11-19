<script setup lang="ts">
import { encode } from 'uqr'
import { computed } from 'vue'

const props = defineProps<{ uri: string; secret: string }>()

// Rendered as vector rectangles, so the secret never leaves the browser.
const qr = computed(() => encode(props.uri, { ecc: 'M', border: 2 }))
const cells = computed(() =>
  qr.value.data.flatMap((row, y) => row.flatMap((dark, x) => (dark ? [{ x, y }] : []))),
)
const groupedSecret = computed(() => props.secret.match(/.{1,4}/g)?.join(' ') ?? props.secret)
</script>

<template>
  <div class="space-y-3">
    <p class="text-sm text-muted">Leia o código com um aplicativo autenticador, como Google Authenticator ou Authy.</p>
    <svg
      class="size-48 rounded-control bg-white"
      :viewBox="`0 0 ${qr.size} ${qr.size}`"
      role="img"
      aria-label="Código QR para configurar o aplicativo autenticador"
      shape-rendering="crispEdges"
    >
      <rect v-for="cell in cells" :key="`${cell.x}-${cell.y}`" :x="cell.x" :y="cell.y" width="1" height="1" fill="#000000" />
    </svg>
    <div>
      <p class="text-sm text-muted">Ou digite esta chave no aplicativo:</p>
      <p class="mt-1 font-mono text-sm break-all text-ink" data-testid="totp-secret" :data-secret="secret">{{ groupedSecret }}</p>
    </div>
  </div>
</template>
