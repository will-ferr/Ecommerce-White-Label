import { createPortalClient } from '@whitelabel/api-client'
import { createAuthStore } from '@whitelabel/auth'

/** Session of the customer in the store being visited. */
export const useStoreAuth = createAuthStore(createPortalClient('/api/v1/auth'))
