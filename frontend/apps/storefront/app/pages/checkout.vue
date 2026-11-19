<script setup lang="ts">
import { ApiError, type Address, type CheckoutSummary, type SavedAddress } from '@whitelabel/api-client'
import { errorMessage } from '@whitelabel/auth'
import { STATES, UiAlert, UiButton, UiInput, UiSkeleton } from '@whitelabel/ui'

const api = useStoreApi()
const auth = useStoreAuth()
const cart = useCartStore()
const money = useMoney()

useSeoMeta({ title: 'Finalizar compra', robots: 'noindex,nofollow' })

const summary = ref<CheckoutSummary>()
const savedAddresses = ref<SavedAddress[]>([])
const shippingId = ref('')
const name = ref('')
const email = ref('')
const address = reactive<Address>({ recipient: '', postalCode: '', street: '', number: '', complement: '', district: '', city: '', state: '' })
const failure = ref<string>()
const fieldErrors = ref<Record<string, string>>({})
const busy = ref(false)
const couponInput = ref('')
const appliedCoupon = ref('')
const couponId = useId()
let idempotencyKey = ''

const COUPON_ERRORS: Record<string, string> = {
  COUPON_NOT_FOUND: 'Cupom não encontrado.',
  COUPON_NOT_STARTED: 'Este cupom ainda não está valendo.',
  COUPON_EXPIRED: 'Este cupom expirou.',
  COUPON_MIN_SUBTOTAL: 'O pedido não atinge o valor mínimo deste cupom.',
  COUPON_EXHAUSTED: 'Este cupom esgotou.',
  COUPON_ALREADY_USED: 'Você já usou este cupom.',
}
const couponMessage = computed(() => (summary.value?.couponError ? (COUPON_ERRORS[summary.value.couponError] ?? 'Não foi possível usar este cupom.') : ''))

async function applyCoupon(code: string) {
  appliedCoupon.value = code.trim()
  try {
    await refresh()
  } catch (error) {
    failure.value = errorMessage(error)
  }
}

function useAddress(saved: SavedAddress) {
  Object.assign(address, {
    recipient: saved.recipient, postalCode: saved.postalCode, street: saved.street, number: saved.number,
    complement: saved.complement, district: saved.district, city: saved.city, state: saved.state,
  })
}

// Options priced by CEP appear once the CEP is complete.
const postalDigits = computed(() => {
  const digits = address.postalCode.replace(/\D/g, '')
  return digits.length === 8 ? digits : ''
})

async function refresh() {
  summary.value = await api.summary({ shippingMethodId: shippingId.value, postalCode: postalDigits.value, couponCode: appliedCoupon.value })
  const offered = summary.value.shippingOptions.some((option) => option.id === shippingId.value)
  if (!offered && summary.value.shippingOptions[0]) {
    shippingId.value = summary.value.shippingOptions[0].id
    summary.value = await api.summary({ shippingMethodId: shippingId.value, postalCode: postalDigits.value, couponCode: appliedCoupon.value })
  }
}

watch(postalDigits, async () => {
  if (!summary.value) return
  try {
    await refresh()
  } catch (error) {
    failure.value = errorMessage(error)
  }
})

onMounted(async () => {
  try {
    await auth.restore()
    if (auth.user) {
      name.value = auth.user.name
      email.value = auth.user.email
      savedAddresses.value = await api.addresses()
      const preferred = savedAddresses.value.find((item) => item.isDefault) ?? savedAddresses.value[0]
      if (preferred) useAddress(preferred)
    }
    await refresh()
  } catch (error) {
    failure.value = errorMessage(error)
  }
})

watch(shippingId, async (value, previous) => {
  if (previous && value !== previous) {
    try {
      await refresh()
    } catch (error) {
      failure.value = errorMessage(error)
    }
  }
})

const FIELD_LABELS: Record<string, string> = {
  customerName: 'Informe seu nome.', customerEmail: 'Informe um e-mail válido.',
  'address.recipient': 'Informe o destinatário.', 'address.postalCode': 'Informe um CEP com 8 dígitos.',
  'address.street': 'Informe a rua.', 'address.number': 'Informe o número, ou S/N.', 'address.district': 'Informe o bairro.',
  'address.city': 'Informe a cidade.', 'address.state': 'Escolha o estado.', 'address.complement': 'Use até 80 caracteres.',
}

async function submit() {
  if (!summary.value) return
  busy.value = true
  failure.value = undefined
  fieldErrors.value = {}
  // The same key is reused if the connection fails, so a retry never creates a second order.
  idempotencyKey ||= crypto.randomUUID()
  try {
    const placed = await api.placeOrder(
      {
        customerName: name.value, customerEmail: email.value, address: { ...address }, shippingMethodId: shippingId.value,
        expectedTotal: summary.value.total, couponCode: summary.value.coupon?.code ?? '',
      },
      idempotencyKey,
    )
    await cart.load().catch(() => undefined)
    await navigateTo({ path: `/pedido/${placed.orderNumber}`, query: { token: placed.orderToken } })
  } catch (error) {
    if (error instanceof ApiError && error.status !== 0) idempotencyKey = ''
    if (error instanceof ApiError && (error.code === 'PRICE_CHANGED' || error.code === 'OUT_OF_STOCK')) {
      const details = error.details as { summary?: CheckoutSummary } | undefined
      if (details?.summary) summary.value = details.summary
      failure.value = error.code === 'PRICE_CHANGED'
        ? 'Os valores mudaram desde que você abriu esta página. Confira o novo total e confirme novamente.'
        : 'Alguns itens esgotaram. Ajuste o carrinho para continuar.'
    } else if (error instanceof ApiError && error.code in COUPON_ERRORS) {
      failure.value = COUPON_ERRORS[error.code]
      await applyCoupon('')
    } else if (error instanceof ApiError && error.fieldErrors.length) {
      fieldErrors.value = Object.fromEntries(error.fieldErrors.map((field) => [field.field, FIELD_LABELS[field.field] ?? 'Confira este campo.']))
      failure.value = 'Confira os campos destacados.'
    } else {
      failure.value = errorMessage(error)
    }
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <StoreLayout>
    <div class="space-y-6">
      <h1 class="font-display text-4xl text-ink">Finalizar compra</h1>
      <UiAlert v-if="failure" :title="failure" />
      <UiSkeleton v-if="!summary" class="h-64 w-full" />

      <div v-else-if="summary.cart.lines.length === 0" class="rounded-card border border-line bg-surface p-6">
        <p class="text-ink">Seu carrinho está vazio. <NuxtLink to="/" class="font-semibold text-primary underline underline-offset-4">Ver produtos</NuxtLink></p>
      </div>

      <form v-else class="grid gap-6 lg:grid-cols-[1fr_22rem]" novalidate @submit.prevent="submit">
        <div class="space-y-6">
          <fieldset class="space-y-4 rounded-card border border-line bg-surface p-5 sm:p-6">
            <legend class="px-1 font-display text-2xl text-ink">Seus dados</legend>
            <UiInput v-model="name" label="Nome completo" autocomplete="name" :error="fieldErrors.customerName" />
            <UiInput v-model="email" label="E-mail" type="email" inputmode="email" autocomplete="email" :error="fieldErrors.customerEmail" />
          </fieldset>

          <fieldset class="space-y-4 rounded-card border border-line bg-surface p-5 sm:p-6">
            <legend class="px-1 font-display text-2xl text-ink">Endereço de entrega</legend>
            <div v-if="savedAddresses.length" class="space-y-1.5">
              <label for="saved-address" class="block text-sm font-semibold text-ink">Endereços salvos</label>
              <select
                id="saved-address"
                class="block min-h-11 w-full rounded-control border border-muted bg-surface px-3 text-ink"
                @change="useAddress(savedAddresses[Number(($event.target as HTMLSelectElement).value)]!)"
              >
                <option v-for="(saved, index) in savedAddresses" :key="saved.id" :value="index">{{ saved.label || saved.street }}, {{ saved.number }}</option>
              </select>
            </div>
            <UiInput v-model="address.recipient" label="Destinatário" autocomplete="name" :error="fieldErrors['address.recipient']" />
            <div class="grid gap-4 sm:grid-cols-2">
              <UiInput v-model="address.postalCode" label="CEP" inputmode="numeric" autocomplete="postal-code" :error="fieldErrors['address.postalCode']" />
              <div class="space-y-1.5">
                <label for="state" class="block text-sm font-semibold text-ink">Estado</label>
                <select id="state" v-model="address.state" autocomplete="address-level1" class="block min-h-11 w-full rounded-control border bg-surface px-3 text-ink" :class="fieldErrors['address.state'] ? 'border-danger' : 'border-muted'">
                  <option value="" disabled>Selecione</option>
                  <option v-for="uf in STATES" :key="uf" :value="uf">{{ uf }}</option>
                </select>
                <p v-if="fieldErrors['address.state']" class="text-sm text-danger">{{ fieldErrors['address.state'] }}</p>
              </div>
            </div>
            <UiInput v-model="address.street" label="Rua" autocomplete="address-line1" :error="fieldErrors['address.street']" />
            <div class="grid gap-4 sm:grid-cols-2">
              <UiInput v-model="address.number" label="Número" :error="fieldErrors['address.number']" />
              <UiInput v-model="address.complement" label="Complemento" autocomplete="address-line2" :required="false" :error="fieldErrors['address.complement']" />
            </div>
            <div class="grid gap-4 sm:grid-cols-2">
              <UiInput v-model="address.district" label="Bairro" :error="fieldErrors['address.district']" />
              <UiInput v-model="address.city" label="Cidade" autocomplete="address-level2" :error="fieldErrors['address.city']" />
            </div>
          </fieldset>

          <fieldset class="space-y-3 rounded-card border border-line bg-surface p-5 sm:p-6">
            <legend class="px-1 font-display text-2xl text-ink">Entrega</legend>
            <p v-if="!summary.shippingOptions.length && !postalDigits" class="text-muted">Informe o CEP para ver as formas de entrega.</p>
            <p v-else-if="!summary.shippingOptions.length" class="text-danger">Não há forma de entrega para este CEP.</p>
            <p v-else-if="!postalDigits" class="text-sm text-muted">Outras opções podem aparecer depois que você informar o CEP.</p>
            <label v-for="option in summary.shippingOptions" :key="option.id" class="flex min-h-11 cursor-pointer items-center justify-between gap-3 rounded-control border border-line p-3">
              <span class="flex items-center gap-3">
                <input v-model="shippingId" type="radio" name="shipping" :value="option.id" class="size-5 accent-[var(--wl-color-primary)]" />
                <span>
                  <span class="block font-semibold text-ink">{{ option.name }}</span>
                  <span class="block text-sm text-muted">{{ option.kind === 'pickup' ? 'Retire na loja' : `De ${option.minDays} a ${option.maxDays} dias úteis` }}</span>
                </span>
              </span>
              <span class="font-semibold text-ink">{{ option.price ? money(option.price) : 'Grátis' }}</span>
            </label>
          </fieldset>

          <section v-if="summary.testPayment" class="space-y-2 rounded-card border border-warning/40 bg-warning-soft p-5 text-warning" aria-labelledby="payment-title">
            <h2 id="payment-title" class="font-semibold">{{ summary.paymentLabel }}</h2>
            <p class="text-sm">Ambiente de demonstração: nenhum valor é cobrado. Você poderá aprovar ou recusar o pagamento na página do pedido.</p>
          </section>
          <UiAlert v-else-if="!summary.paymentLabel" title="Esta loja ainda não está recebendo pagamentos." />
        </div>

        <aside class="h-fit space-y-4 rounded-card border border-line bg-surface p-5" aria-label="Resumo do pedido">
          <h2 class="font-display text-2xl text-ink">Resumo</h2>
          <ul class="space-y-2 text-sm">
            <li v-for="line in summary.cart.lines" :key="line.variantId" class="flex justify-between gap-3">
              <span class="text-ink">{{ line.quantity }}× {{ line.productName }}<span v-if="line.label" class="text-muted"> ({{ line.label }})</span></span>
              <span class="shrink-0 text-ink">{{ money(line.total) }}</span>
            </li>
          </ul>
          <dl class="space-y-2 border-t border-line pt-4 text-ink">
            <div class="flex justify-between"><dt>Subtotal</dt><dd>{{ money(summary.subtotal) }}</dd></div>
            <div class="flex justify-between"><dt>Frete</dt><dd>{{ summary.shipping ? money(summary.shippingAmount) : '—' }}</dd></div>
            <div v-if="summary.discount" class="flex justify-between text-success" data-testid="checkout-discount">
              <dt>Desconto ({{ summary.coupon?.code }})</dt><dd>−{{ money(summary.discount) }}</dd>
            </div>
            <div class="flex justify-between text-lg font-semibold"><dt>Total</dt><dd data-testid="checkout-total">{{ money(summary.total) }}</dd></div>
          </dl>
          <div class="space-y-1.5 border-t border-line pt-4">
            <label :for="couponId" class="block text-sm font-semibold text-ink">Cupom de desconto</label>
            <div v-if="summary.coupon" class="flex items-center justify-between gap-3 text-sm">
              <span class="text-ink"><strong>{{ summary.coupon.code }}</strong><span v-if="summary.coupon.description" class="text-muted"> · {{ summary.coupon.description }}</span></span>
              <button type="button" class="min-h-11 font-semibold text-primary underline underline-offset-4" @click="applyCoupon('')">Remover cupom</button>
            </div>
            <div v-else class="flex gap-2">
              <input
                :id="couponId" v-model="couponInput" maxlength="30" autocomplete="off" :aria-invalid="couponMessage ? 'true' : undefined"
                :aria-describedby="couponMessage ? `${couponId}-erro` : undefined"
                class="block min-h-11 w-full min-w-0 rounded-control border border-muted bg-surface px-3 uppercase text-ink"
                @keydown.enter.prevent="applyCoupon(couponInput)"
              />
              <button type="button" class="min-h-11 shrink-0 rounded-control border border-primary px-4 font-semibold text-primary" @click="applyCoupon(couponInput)">Aplicar</button>
            </div>
            <p v-if="couponMessage" :id="`${couponId}-erro`" class="text-sm text-danger" role="status">{{ couponMessage }}</p>
          </div>
          <p class="text-xs text-muted">Impostos não calculados: a configuração fiscal da loja ainda não foi validada.</p>
          <UiButton type="submit" class="w-full" :disabled="busy || !summary.shipping || !summary.paymentLabel || summary.cart.hasUnavailable">
            {{ busy ? 'Enviando…' : 'Confirmar pedido' }}
          </UiButton>
        </aside>
      </form>
    </div>
  </StoreLayout>
</template>
