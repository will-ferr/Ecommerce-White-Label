<script setup lang="ts">
const tenant = useTenantStore()
const client = useApiClient()

await callOnce('tenant-config', () => tenant.load(client))

if (tenant.failure) {
  throw createError({
    statusCode: tenant.failure.status >= 400 ? tenant.failure.status : 503,
    statusMessage: tenant.failure.code,
    fatal: true,
  })
}

useTenantTheme()
</script>

<template>
  <NuxtPage />
</template>
