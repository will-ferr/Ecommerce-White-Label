import { defineStore } from 'pinia'
import { ref } from 'vue'

const GENERIC_MESSAGE = 'Ocorreu um erro inesperado. Recarregue a página e tente novamente.'

/** Central place for failures that no screen handled by itself. */
export const useErrorStore = defineStore('errors', () => {
  const message = ref<string>()

  function report(error: unknown) {
    console.error(error)
    message.value = GENERIC_MESSAGE
  }

  function dismiss() {
    message.value = undefined
  }

  return { message, report, dismiss }
})
