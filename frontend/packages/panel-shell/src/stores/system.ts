import { ApiError, createApiClient, type ApiClient } from '@whitelabel/api-client'
import type { SystemCardState } from '@whitelabel/ui'
import { defineStore } from 'pinia'
import { ref } from 'vue'

export const useSystemStore = defineStore('system', () => {
  const state = ref<SystemCardState>('loading')
  const version = ref<string>()
  const errorMessage = ref<string>()
  const correlationId = ref<string>()

  async function load(client: ApiClient = createApiClient()) {
    state.value = 'loading'
    errorMessage.value = undefined
    correlationId.value = undefined
    try {
      const status = await client.getStatus()
      state.value = status.status
      version.value = status.version
    } catch (error) {
      if (!(error instanceof ApiError)) throw error
      state.value = 'error'
      errorMessage.value = error.message
      correlationId.value = error.correlationId
    }
  }

  return { state, version, errorMessage, correlationId, load }
})
