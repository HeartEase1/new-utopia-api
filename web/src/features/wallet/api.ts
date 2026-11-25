/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { api } from '@/lib/api'
import { createServerError } from '@/lib/server-error-message'

import type {
  RedemptionRequest,
  PaymentRequest,
  AmountRequest,
  AffiliateTransferRequest,
  ApiResponse,
  TopupInfoResponse,
  RedemptionResponse,
  AmountResponse,
  PaymentResponse,
  StripePaymentResponse,
  AffiliateCodeResponse,
  AffiliateTransferResponse,
  BillingHistoryResponse,
  CompleteOrderRequest,
  CreemPaymentRequest,
  CreemPaymentResponse,
  WaffoPaymentRequest,
  WaffoPaymentResponse,
  WaffoPancakePaymentRequest,
  WaffoPancakePaymentResponse,
  WalletTransferRecipient,
  WalletTransferStatus,
  WalletTransferQuoteResult,
  WalletTransferRecord,
  WalletTransferPasswordFlow,
} from './types'

// ============================================================================
// Wallet API Functions
// ============================================================================

export interface AlipayF2FOrder {
  trade_no: string
  qr_code: string
  amount: string
  expires_at: number
}

export async function requestAlipayF2F(
  amount: number
): Promise<AlipayF2FOrder> {
  const res = await api.post('/api/user/alipay-f2f/pay', { amount })
  if (!res.data.success) {
    throw createServerError(res.data, 'Payment request failed')
  }
  return res.data.data
}

function requireWalletSuccess<T>(
  response: ApiResponse<T>,
  fallback: string
): T {
  if (!response.success || response.data === undefined) {
    throw createServerError(response, fallback)
  }
  return response.data
}

export async function getWalletTransferStatus(): Promise<WalletTransferStatus> {
  const res = await api.get('/api/user/wallet-transfer/status')
  return requireWalletSuccess(res.data, 'Failed to load transfer settings')
}

export async function searchWalletTransferRecipients(
  keyword: string
): Promise<WalletTransferRecipient[]> {
  const res = await api.get('/api/user/wallet-transfer/recipients', {
    params: { keyword },
  })
  return requireWalletSuccess(res.data, 'Failed to search users')
}

export async function createWalletTransferQuote(
  recipientId: number,
  amount: number
): Promise<WalletTransferQuoteResult> {
  const res = await api.post('/api/user/wallet-transfer/quote', {
    recipient_id: recipientId,
    amount,
  })
  return requireWalletSuccess(res.data, 'Failed to prepare transfer')
}

export async function confirmWalletTransfer(request: {
  quote_token: string
  login_password: string
  payment_password: string
  two_fa_code?: string
}): Promise<WalletTransferRecord> {
  const res = await api.post('/api/user/wallet-transfer/confirm', request, {
    singleUseAuthorization: true,
  })
  return requireWalletSuccess(res.data, 'Transfer failed')
}

export async function startWalletTransferPasswordFlow(
  intent: 'set' | 'change' | 'reset'
): Promise<WalletTransferPasswordFlow> {
  const res = await api.post('/api/user/wallet-transfer/password/start', {
    intent,
  })
  return requireWalletSuccess(res.data, 'Failed to send email verification')
}

export async function completeWalletTransferPasswordFlow(request: {
  flow_token: string
  code: string
  current_password?: string
  login_password?: string
  two_fa_code?: string
  new_password: string
}): Promise<void> {
  const res = await api.post(
    '/api/user/wallet-transfer/password/complete',
    request,
    { singleUseAuthorization: true }
  )
  requireWalletSuccess(res.data, 'Failed to save payment password')
}

export async function getWalletTransferHistory(
  page: number,
  pageSize: number
): Promise<{ items: WalletTransferRecord[]; total: number }> {
  const params = new URLSearchParams({
    p: String(page),
    page_size: String(pageSize),
  })
  const res = await api.get(`/api/user/wallet-transfer/history?${params}`)
  return requireWalletSuccess(res.data, 'Failed to load transfer history')
}

export async function getAdminWalletTransfers(
  page: number,
  pageSize: number,
  keyword: string,
  date: string
): Promise<{
  page: { items: WalletTransferRecord[]; total: number }
  total_fee: number
}> {
  const params = new URLSearchParams({
    p: String(page),
    page_size: String(pageSize),
  })
  if (keyword) params.set('keyword', keyword)
  if (date) params.set('date', date)
  const res = await api.get(`/api/user/wallet-transfers?${params}`)
  return requireWalletSuccess(res.data, 'Failed to load transfer records')
}

export async function queryAlipayF2FOrder(
  tradeNo: string,
  signal?: AbortSignal
): Promise<string> {
  const res = await api.post(
    '/api/user/alipay-f2f/query',
    { trade_no: tradeNo },
    { signal }
  )
  if (!res.data.success) {
    throw createServerError(res.data, 'Failed to query payment status')
  }
  return res.data.data.status
}

export async function cancelAlipayF2FOrder(tradeNo: string): Promise<string> {
  const res = await api.post('/api/user/alipay-f2f/cancel', {
    trade_no: tradeNo,
  })
  if (!res.data.success) {
    throw createServerError(
      res.data,
      'Unable to cancel payment. Check the payment status and try again.'
    )
  }
  return res.data.data.status
}

/**
 * Check if API response is successful
 */
export function isApiSuccess(response: ApiResponse): boolean {
  return response.success === true || response.message === 'success'
}

/**
 * Get topup configuration info
 */
export async function getTopupInfo(): Promise<TopupInfoResponse> {
  const res = await api.get('/api/user/topup/info')
  return res.data
}

/**
 * Redeem a topup code
 */
export async function redeemTopupCode(
  request: RedemptionRequest
): Promise<RedemptionResponse> {
  const res = await api.post('/api/user/topup', request)
  return res.data
}

/**
 * Calculate payment amount for regular payment
 */
export async function calculateAmount(
  request: AmountRequest
): Promise<AmountResponse> {
  const res = await api.post('/api/user/amount', request, {
    skipBusinessError: true,
  } as Record<string, unknown>)
  return res.data
}

/**
 * Calculate payment amount for Stripe payment
 */
export async function calculateStripeAmount(
  request: AmountRequest
): Promise<AmountResponse> {
  const res = await api.post('/api/user/stripe/amount', request, {
    skipBusinessError: true,
  } as Record<string, unknown>)
  return res.data
}

/**
 * Calculate payment amount for Waffo payment
 */
export async function calculateWaffoAmount(
  request: AmountRequest
): Promise<AmountResponse> {
  const res = await api.post('/api/user/waffo/amount', request, {
    skipBusinessError: true,
  } as Record<string, unknown>)
  return res.data
}

/**
 * Request regular payment
 */
export async function requestPayment(
  request: PaymentRequest
): Promise<PaymentResponse> {
  const res = await api.post('/api/user/pay', request, {
    skipBusinessError: true,
  } as Record<string, unknown>)
  return {
    ...res.data,
    url: res.data.url || (res as unknown as { url?: string }).url,
  }
}

/**
 * Request Stripe payment
 */
export async function requestStripePayment(
  request: PaymentRequest
): Promise<StripePaymentResponse> {
  const res = await api.post('/api/user/stripe/pay', request, {
    skipBusinessError: true,
  } as Record<string, unknown>)
  return res.data
}

/**
 * Request Creem payment
 */
export async function requestCreemPayment(
  request: CreemPaymentRequest
): Promise<CreemPaymentResponse> {
  const res = await api.post('/api/user/creem/pay', request, {
    skipBusinessError: true,
  } as Record<string, unknown>)
  return res.data
}

/**
 * Request Waffo payment
 */
export async function requestWaffoPayment(
  request: WaffoPaymentRequest
): Promise<WaffoPaymentResponse> {
  const res = await api.post('/api/user/waffo/pay', request, {
    skipBusinessError: true,
  } as Record<string, unknown>)
  return res.data
}

/**
 * Calculate payment amount for Waffo Pancake payment
 */
export async function calculateWaffoPancakeAmount(
  request: AmountRequest
): Promise<AmountResponse> {
  const res = await api.post('/api/user/waffo-pancake/amount', request, {
    skipBusinessError: true,
  } as Record<string, unknown>)
  return res.data
}

/**
 * Request Waffo Pancake payment
 */
export async function requestWaffoPancakePayment(
  request: WaffoPancakePaymentRequest
): Promise<WaffoPancakePaymentResponse> {
  const res = await api.post('/api/user/waffo-pancake/pay', request, {
    skipBusinessError: true,
  } as Record<string, unknown>)
  return res.data
}

/**
 * Get affiliate code
 */
export async function getAffiliateCode(): Promise<AffiliateCodeResponse> {
  const res = await api.get('/api/user/aff')
  return res.data
}

/**
 * Transfer affiliate quota to balance
 */
export async function transferAffiliateQuota(
  request: AffiliateTransferRequest
): Promise<AffiliateTransferResponse> {
  const res = await api.post('/api/user/aff_transfer', request)
  return res.data
}

/**
 * Get billing history for current user
 */
export async function getUserBillingHistory(
  page: number,
  pageSize: number,
  keyword?: string
): Promise<ApiResponse<BillingHistoryResponse>> {
  const params = new URLSearchParams({
    p: page.toString(),
    page_size: pageSize.toString(),
  })
  if (keyword) {
    params.append('keyword', keyword)
  }
  const res = await api.get(`/api/user/topup/self?${params.toString()}`)
  return res.data
}

/**
 * Get billing history for all users (admin only)
 */
export async function getAllBillingHistory(
  page: number,
  pageSize: number,
  keyword?: string
): Promise<ApiResponse<BillingHistoryResponse>> {
  const params = new URLSearchParams({
    p: page.toString(),
    page_size: pageSize.toString(),
  })
  if (keyword) {
    params.append('keyword', keyword)
  }
  const res = await api.get(`/api/user/topup?${params.toString()}`)
  return res.data
}

/**
 * Complete a pending order (admin only)
 */
export async function completeOrder(
  request: CompleteOrderRequest
): Promise<ApiResponse> {
  const res = await api.post('/api/user/topup/complete', request)
  return res.data
}
