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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createInstance } from 'i18next'
import { I18nextProvider, initReactI18next } from 'react-i18next'
import { beforeEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import { AlipayF2FDialog } from '../components/dialogs/alipay-f2f-dialog'
import { buildAlipayDeepLink, isAlipayLaunchRestricted } from '../lib/alipay'
import { getMinTopupAmount } from '../lib/payment'
import type { TopupInfo } from '../types'

const i18n = createInstance()
beforeEach(async () => {
  await i18n
    .use(initReactI18next)
    .init({
      lng: 'en',
      fallbackLng: false,
      resources: { en: { translation: {} } },
      interpolation: { escapeValue: false },
    })
})

function showDialog(expiresAt = Date.now() / 1000 + 900) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  })
  const onPaid = vi.fn()
  const onClose = vi.fn()
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={client}>
        <AlipayF2FDialog
          order={{
            trade_no: 'UTtest',
            qr_code: 'https://qr.alipay.com/example',
            amount: '10.01',
            expires_at: expiresAt,
          }}
          onPaid={onPaid}
          onClose={onClose}
        />
      </QueryClientProvider>
    </I18nextProvider>
  )
  return { onPaid, onClose }
}

it('keeps a pending payment uncredited and displays a QR code and app launch button', async () => {
  vi.spyOn(api, 'post').mockResolvedValue({
    data: { success: true, data: { status: 'pending' } },
  })
  const { onPaid } = showDialog()
  expect(
    screen.getByRole('img', { name: 'Alipay payment QR code' })
  ).toBeVisible()
  expect(screen.getByRole('button', { name: 'Open Alipay' })).toBeEnabled()
  await waitFor(() =>
    expect(
      screen.getByRole('button', { name: 'Check payment status' })
    ).toBeEnabled()
  )
  expect(screen.getByText('CNY 10.01')).toBeVisible()
  expect(onPaid).not.toHaveBeenCalled()
})

it('shows a retry action after a failed query and refreshes the balance only after server-confirmed payment', async () => {
  vi.spyOn(api, 'post')
    .mockRejectedValueOnce(new Error('offline'))
    .mockResolvedValueOnce({
      data: { success: true, data: { status: 'success' } },
    })
  const { onPaid } = showDialog()
  expect(await screen.findByRole('alert')).toHaveTextContent(
    'Unable to check payment'
  )
  expect(onPaid).not.toHaveBeenCalled()
  const user = userEvent.setup()
  const retry = screen.getByRole('button', { name: 'Check payment status' })
  retry.focus()
  await user.keyboard('{Enter}')
  expect(await screen.findByText('Payment received')).toBeVisible()
  await waitFor(() => expect(onPaid).toHaveBeenCalledTimes(1))
  expect(
    screen.queryByRole('button', { name: 'Open Alipay' })
  ).not.toBeInTheDocument()
})

it('hides an expired QR code but still allows querying a payment made before expiry', async () => {
  vi.spyOn(api, 'post').mockResolvedValue({
    data: { success: true, data: { status: 'pending' } },
  })
  showDialog(1)
  expect(
    screen.queryByRole('img', { name: 'Alipay payment QR code' })
  ).not.toBeInTheDocument()
  expect(
    screen.getByText(
      'This QR code has expired. If you already paid, check the payment status.'
    )
  ).toBeVisible()
  await waitFor(() =>
    expect(
      screen.getByRole('button', { name: 'Check payment status' })
    ).toBeEnabled()
  )
})

it('rejects unsafe app launch payloads and identifies browsers requiring fallback', () => {
  expect(buildAlipayDeepLink('https://qr.alipay.com/example')).toContain(
    encodeURIComponent('https://qr.alipay.com/example')
  )
  for (const value of [
    'javascript:alert(1)',
    'https://evil.example/pay',
    'https://qr.alipay.com.evil.example/pay',
    'https://user@qr.alipay.com/pay',
    '',
  ]) {
    expect(buildAlipayDeepLink(value)).toBeNull()
  }
  expect(isAlipayLaunchRestricted('MicroMessenger')).toBe(true)
  expect(isAlipayLaunchRestricted('Mozilla Safari')).toBe(false)
})

it('uses the configured recharge minimum when only Alipay F2F is enabled', () => {
  const info = {
    enable_alipay_f2f_topup: true,
    enable_online_topup: false,
    min_topup: 7,
  } as TopupInfo
  expect(getMinTopupAmount(info)).toBe(7)
})
