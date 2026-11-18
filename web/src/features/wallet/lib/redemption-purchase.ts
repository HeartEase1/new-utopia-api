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

export const redemptionPurchaseSchema = z.object({
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

export type RedemptionPurchase = z.infer<typeof redemptionPurchaseSchema>

export function parseRedemptionPurchase(raw?: string): RedemptionPurchase {
  const empty = { name: '', url: '', fee: '', payment_methods: '' }
  if (!raw?.trim()) return empty
  try {
    const parsed: unknown = JSON.parse(raw)
    if (!parsed || typeof parsed !== 'object') return empty
    // Keep incomplete fields editable; validation happens when saving or linking.
    for (const key of Object.keys(empty) as (keyof RedemptionPurchase)[]) {
      if (
        key in parsed &&
        typeof parsed[key as keyof typeof parsed] === 'string'
      ) {
        empty[key] = parsed[key as keyof typeof parsed]
      }
    }
    return empty
  } catch {
    return empty
  }
}
