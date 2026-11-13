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
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createInstance } from 'i18next'
import { useState } from 'react'
import { I18nextProvider, initReactI18next } from 'react-i18next'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import { AlipayF2FDialog } from '../components/dialogs/alipay-f2f-dialog'
import { buildAlipayDeepLink, isAlipayLaunchRestricted } from '../lib/alipay'
import { getMinTopupAmount } from '../lib/payment'
import type { TopupInfo } from '../types'

const i18n = createInstance()
afterEach(() => {
  cleanup()
  vi.useRealTimers()
})
beforeEach(async () => {
  await i18n.use(initReactI18next).init({
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
  function Payment() {
    const [open, setOpen] = useState(true)
    if (!open) return null
    return (
      <AlipayF2FDialog
        order={{
          trade_no: 'UTtest',
          qr_code: 'https://qr.alipay.com/example',
          amount: '10.01',
          expires_at: expiresAt,
        }}
        onPaid={onPaid}
        onClose={() => {
          onClose()
          setOpen(false)
        }}
      />
    )
  }
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={client}>
        <Payment />
      </QueryClientProvider>
    </I18nextProvider>
  )
  return { onPaid, onClose }
}

it('waits for Alipay cancellation before closing and prevents duplicate cancellation', async () => {
  let finishCancel: ((value: unknown) => void) | undefined
  const post = vi.spyOn(api, 'post').mockImplementation((url) => {
    if (url === '/api/user/alipay-f2f/cancel') {
      return new Promise((resolve) => {
        finishCancel = resolve
      })
    }
    return Promise.resolve({
      data: { success: true, data: { status: 'pending' } },
    })
  })
  const { onClose } = showDialog()
  fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
  await waitFor(() =>
    expect(post).toHaveBeenCalledWith('/api/user/alipay-f2f/cancel', {
      trade_no: 'UTtest',
    })
  )
  expect(onClose).not.toHaveBeenCalled()
  expect(
    screen.getByRole('button', { name: 'Cancelling payment...' })
  ).toBeDisabled()
  await act(async () =>
    finishCancel?.({ data: { success: true, data: { status: 'expired' } } })
  )
  await waitFor(() => expect(onClose).toHaveBeenCalledTimes(1))
})

it('keeps the dialog open after cancellation failure and allows retry', async () => {
  vi.spyOn(api, 'post').mockImplementation((url) => {
    if (url === '/api/user/alipay-f2f/cancel') {
      return Promise.reject(new Error('offline'))
    }
    return Promise.resolve({
      data: { success: true, data: { status: 'pending' } },
    })
  })
  const { onClose } = showDialog()
  fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
  expect(await screen.findByRole('alert')).toHaveTextContent(
    'Unable to cancel payment'
  )
  expect(onClose).not.toHaveBeenCalled()
  expect(screen.getByRole('button', { name: 'Cancel' })).toBeEnabled()
})

it('refreshes the balance if payment wins the race with cancellation', async () => {
  vi.spyOn(api, 'post').mockImplementation((url) =>
    Promise.resolve({
      data: {
        success: true,
        data: {
          status: url === '/api/user/alipay-f2f/cancel' ? 'success' : 'pending',
        },
      },
    })
  )
  const { onPaid, onClose } = showDialog()
  fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
  await waitFor(() => expect(onPaid).toHaveBeenCalledTimes(1))
  expect(onClose).toHaveBeenCalledTimes(1)
})

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

it('polls every five seconds for two minutes then only queries when requested', async () => {
  vi.useFakeTimers()
  const post = vi.spyOn(api, 'post').mockResolvedValue({
    data: { success: true, data: { status: 'pending' } },
  })
  showDialog()
  await act(() => vi.advanceTimersByTimeAsync(0))
  expect(post).toHaveBeenCalledTimes(1)
  await act(() => vi.advanceTimersByTimeAsync(5000))
  expect(post).toHaveBeenCalledTimes(2)
  for (let i = 0; i < 22; i++) {
    await act(() => vi.advanceTimersByTimeAsync(5000))
  }
  expect(post).toHaveBeenCalledTimes(24)
  await act(() => vi.advanceTimersByTimeAsync(5000))
  const stoppedAt = post.mock.calls.length
  await act(() => vi.advanceTimersByTimeAsync(180000))
  expect(post).toHaveBeenCalledTimes(stoppedAt)
  expect(
    screen.getByText(
      'Automatic checking has stopped. After paying, click Check payment status.'
    )
  ).toBeVisible()
  expect(
    screen.getByRole('img', { name: 'Alipay payment QR code' })
  ).toBeVisible()
  fireEvent.click(screen.getByRole('button', { name: 'Check payment status' }))
  await act(() => vi.advanceTimersByTimeAsync(0))
  expect(post).toHaveBeenCalledTimes(stoppedAt + 1)
  await act(() => vi.advanceTimersByTimeAsync(15000))
  expect(post).toHaveBeenCalledTimes(stoppedAt + 1)
})

it('counts down from the order deadline without querying on every tick', async () => {
  vi.useFakeTimers()
  vi.setSystemTime(new Date('2026-11-13T00:00:00Z'))
  const post = vi.spyOn(api, 'post').mockResolvedValue({
    data: { success: true, data: { status: 'pending' } },
  })
  showDialog(Date.now() / 1000 + 300)
  await act(() => vi.advanceTimersByTimeAsync(0))
  expect(screen.getByRole('timer')).toHaveTextContent('05:00')
  await act(() => vi.advanceTimersByTimeAsync(1000))
  expect(screen.getByRole('timer')).toHaveTextContent('04:59')
  expect(post).toHaveBeenCalledTimes(1)

  // Mobile browsers suspend timers while the user is in Alipay.
  vi.setSystemTime(new Date('2026-11-13T00:04:58Z'))
  fireEvent(document, new Event('visibilitychange'))
  expect(screen.getByRole('timer')).toHaveTextContent('00:02')
  await act(() => vi.advanceTimersByTimeAsync(2000))
  expect(screen.queryByRole('timer')).not.toBeInTheDocument()
  expect(
    screen.queryByRole('img', { name: 'Alipay payment QR code' })
  ).not.toBeInTheDocument()
  expect(
    screen.getByText(
      'This QR code has expired. If you already paid, check the payment status.'
    )
  ).toBeVisible()
  expect(post).toHaveBeenCalledTimes(1)
  fireEvent.click(screen.getByRole('button', { name: 'Check payment status' }))
  await act(() => vi.advanceTimersByTimeAsync(0))
  expect(post).toHaveBeenCalledTimes(2)
})

it('keeps the payment dialog open after outside clicks and Escape until Cancel is pressed', async () => {
  vi.spyOn(api, 'post').mockResolvedValue({
    data: { success: true, data: { status: 'pending' } },
  })
  const { onClose } = showDialog()
  const user = userEvent.setup()
  const backdrop = document.querySelector<HTMLElement>(
    '[data-slot="dialog-overlay"]'
  )
  expect(backdrop).not.toBeNull()
  if (!backdrop) throw new Error('Missing dialog backdrop')
  await user.click(backdrop)
  expect(onClose).not.toHaveBeenCalled()
  await user.keyboard('{Escape}')
  expect(onClose).not.toHaveBeenCalled()
  expect(screen.getByRole('dialog')).toBeVisible()
  expect(
    screen.queryByRole('button', { name: 'Close' })
  ).not.toBeInTheDocument()
  await user.click(screen.getByRole('button', { name: 'Cancel' }))
  await waitFor(() => expect(onClose).toHaveBeenCalledTimes(1))
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
})

it('stops automatic queries when Cancel closes the dialog', async () => {
  vi.useFakeTimers()
  const post = vi.spyOn(api, 'post').mockResolvedValue({
    data: { success: true, data: { status: 'pending' } },
  })
  const { onClose } = showDialog()
  await act(() => vi.advanceTimersByTimeAsync(5000))
  const beforeClose = post.mock.calls.length
  fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
  await act(() => vi.advanceTimersByTimeAsync(0))
  expect(onClose).toHaveBeenCalledTimes(1)
  await act(() => vi.advanceTimersByTimeAsync(180000))
  expect(post).toHaveBeenCalledTimes(beforeClose + 1)
})

it('aborts an in-flight status request when Cancel closes the dialog', async () => {
  let signal: AbortSignal | undefined
  vi.spyOn(api, 'post').mockImplementation((url, _body, config) => {
    if (url === '/api/user/alipay-f2f/cancel') {
      return Promise.resolve({
        data: { success: true, data: { status: 'expired' } },
      })
    }
    signal = config?.signal as AbortSignal | undefined
    return new Promise(() => {})
  })
  showDialog()
  await waitFor(() => expect(signal).toBeDefined())
  fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
  await waitFor(() => expect(signal?.aborted).toBe(true))
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
  expect(screen.queryByRole('timer')).not.toBeInTheDocument()
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
