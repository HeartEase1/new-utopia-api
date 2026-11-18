import { z } from 'zod'

export function isPurchaseUrl(value: string): boolean {
  try {
    const url = new URL(value)
    return (
      /^https?:\/\//.test(value) &&
      !!url.hostname &&
      !url.username &&
      !url.password &&
      !/[\\\s]/.test(value)
    )
  } catch {
    return false
  }
}

export const redemptionStoreSchema = z.object({
  name: z.string().max(80, 'Invalid redemption purchase settings').default(''),
  url: z
    .string()
    .max(2048, 'Invalid redemption purchase settings')
    .refine(
      (value) => value === '' || isPurchaseUrl(value),
      'Purchase link must be an HTTP or HTTPS URL without credentials'
    )
    .default(''),
  fee: z.string().max(200, 'Invalid redemption purchase settings').default(''),
  payment_methods: z
    .string()
    .max(200, 'Invalid redemption purchase settings')
    .default(''),
})

export type RedemptionStore = z.infer<typeof redemptionStoreSchema>

export const redemptionStoreListSchema = z
  .array(redemptionStoreSchema)
  .max(20, 'Invalid redemption purchase settings')

export const MAX_REDEMPTION_STORES = 20

const emptyStore: RedemptionStore = {
  name: '',
  url: '',
  fee: '',
  payment_methods: '',
}

function normalizeStore(value: unknown): RedemptionStore | null {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return null
  const store = { ...emptyStore }
  // Keep incomplete fields editable; validation happens when saving or linking.
  for (const key of Object.keys(emptyStore) as (keyof RedemptionStore)[]) {
    const field = (value as Record<string, unknown>)[key]
    if (typeof field === 'string') store[key] = field
  }
  return store
}

/**
 * Reads the saved storefront setting. Sites that saved the earlier single-store
 * object still load it as a one-item list.
 */
export function parseRedemptionStores(raw?: unknown): RedemptionStore[] {
  if (Array.isArray(raw)) {
    return raw
      .map(normalizeStore)
      .filter((store): store is RedemptionStore => store !== null)
      .slice(0, MAX_REDEMPTION_STORES)
  }
  if (typeof raw === 'string') {
    if (!raw.trim()) return []
    try {
      return parseRedemptionStores(JSON.parse(raw) as unknown)
    } catch {
      return []
    }
  }
  const single = normalizeStore(raw)
  return single ? [single] : []
}

export function serializeRedemptionStores(stores: RedemptionStore[]): string {
  // Always emit an array: the backend clears the list from the JSON value, and
  // an empty string would leave the previous stores in memory.
  return JSON.stringify(stores, null, 2)
}
