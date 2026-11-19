import type { AuthResult, PortalClient, UserView } from '@whitelabel/api-client'
import { defineStore } from 'pinia'
import { computed, ref } from 'vue'

const SESSION_HINT_KEY = 'wl.session'

function readHint(): boolean {
  try {
    return globalThis.localStorage?.getItem(SESSION_HINT_KEY) === '1'
  } catch {
    return false
  }
}

function writeHint(active: boolean) {
  try {
    if (active) globalThis.localStorage?.setItem(SESSION_HINT_KEY, '1')
    else globalThis.localStorage?.removeItem(SESSION_HINT_KEY)
  } catch {
    // The session still works for this page when storage is blocked.
  }
}

/** Creates the session store of a portal. */
export function createAuthStore(client: PortalClient) {
  return defineStore('auth', () => {
    const user = ref<UserView | null>(null)
    let restoring: Promise<void> | undefined
    const isAuthenticated = computed(() => user.value !== null)

    function accept(result: AuthResult) {
      if (result.status === 'authenticated' && result.user) {
        user.value = result.user
        writeHint(true)
      }
    }

    /** Recovers the session after a page load, only when one may exist. */
    function restore() {
      restoring ??= (async () => {
        if (!readHint()) return
        try {
          accept(await client.refresh())
        } catch {
          writeHint(false)
        }
      })()
      return restoring
    }

    async function reload() {
      user.value = await client.me()
    }

    async function logout() {
      try {
        await client.logout()
      } finally {
        user.value = null
        writeHint(false)
      }
    }

    function can(permission: string) {
      return user.value?.permissions.includes(permission) ?? false
    }

    return { user, isAuthenticated, client: () => client, accept, restore, reload, logout, can }
  })
}

export type AuthStore = ReturnType<ReturnType<typeof createAuthStore>>
