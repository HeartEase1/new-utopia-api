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
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactNode } from 'react'
import { afterEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import { WalletTransferDialog } from '../components/dialogs/wallet-transfer-dialog'

vi.mock('@tanstack/react-router', () => ({
  Link: (props: { to: string; children?: ReactNode }) => (
    <a href={props.to}>{props.children}</a>
  ),
}))

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

const QUOTE_URL = '/api/user/wallet-transfer/quote'
const CONFIRM_URL = '/api/user/wallet-transfer/confirm'

// Mounts the dialog for a sender whose every review is answered with the
// recipient "bob" (ID 42); `confirm` decides how a submission ends.
function openTransferDialog(options: {
  twoFAEnabled: boolean
  hasPaymentPassword?: boolean
  confirm?: () => Promise<unknown>
}) {
  vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (url === '/api/user/wallet-transfer/recipients') {
      return {
        data: {
          success: true,
          data: [{ id: 42, username: 'bob', display_name: '' }],
        },
      }
    }
    return {
      data: {
        success: true,
        data: {
          has_login_password: true,
          has_email: true,
          email: 'u***@example.com',
          has_payment_password: options.hasPaymentPassword ?? true,
          two_fa_enabled: options.twoFAEnabled,
          attempt_count: 0,
          attempt_limit: options.twoFAEnabled ? 100 : 10,
          used_quota: 0,
          daily_limit_quota: 50_000_000,
          daily_limit_amount: 100,
          display_type: 'USD',
          display_symbol: '$',
        },
      },
    }
  })
  let reviews = 0
  const post = vi.spyOn(api, 'post').mockImplementation(async (url, body) => {
    if (url === CONFIRM_URL && options.confirm) return options.confirm()
    if (url === QUOTE_URL) {
      reviews += 1
      const amount = (body as { amount: number }).amount
      return {
        data: {
          success: true,
          data: {
            quote_token: `review-${reviews}`,
            quote: {
              recipient_id: 42,
              recipient_username: 'bob',
              recipient_display_name: '',
              amount,
              fee: 0,
              received: amount,
              fee_percent: '0.00',
              daily_limit_quota: 50_000_000,
              daily_limit_amount: 100,
              display_type: 'USD',
              display_symbol: '$',
              expires_at: Math.floor(Date.now() / 1000) + 300,
            },
          },
        },
      }
    }
    return { data: { success: true, data: { id: 1 } } }
  })
  const onOpenChange = vi.fn()
  const onTransferred = vi.fn()
  render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <WalletTransferDialog
        open
        onOpenChange={onOpenChange}
        availableQuota={10_000_000}
        onSetPaymentPassword={vi.fn()}
        onTransferred={onTransferred}
      />
    </QueryClientProvider>
  )
  return { post, onOpenChange, onTransferred }
}

// Picks bob, reviews an amount and types both passwords.
async function reviewTransferToBob(user: ReturnType<typeof userEvent.setup>) {
  await user.type(
    await screen.findByLabelText('Recipient username or user ID'),
    'bob'
  )
  await user.click(await screen.findByRole('button', { name: /@bob/ }))
  await user.type(screen.getByLabelText('Transfer amount'), '2')
  await user.click(screen.getByRole('button', { name: 'Review transfer' }))
  await user.type(await screen.findByLabelText('Login password'), 'login-pw')
  await user.type(screen.getByLabelText('Payment password'), 'pay-pw')
}

it('advises enabling 2FA with a link when the sender has none', async () => {
  openTransferDialog({ twoFAEnabled: false })

  expect(
    await screen.findByText('Enable 2FA for higher transfer limits')
  ).toBeVisible()
  expect(screen.getByRole('link', { name: 'Enable 2FA' })).toHaveAttribute(
    'href',
    '/security'
  )
})

it('blocks reviewing a transfer until a payment password is set', async () => {
  openTransferDialog({ twoFAEnabled: false, hasPaymentPassword: false })
  const user = userEvent.setup()

  await user.type(
    await screen.findByLabelText('Recipient username or user ID'),
    'bob'
  )
  await user.click(await screen.findByRole('button', { name: /@bob/ }))
  await user.type(screen.getByLabelText('Transfer amount'), '2')

  expect(screen.getByText('Payment password required')).toBeVisible()
  expect(screen.getByRole('button', { name: 'Review transfer' })).toBeDisabled()
})

it('shows the recipient ID and username, then transfers after a second confirmation', async () => {
  const view = openTransferDialog({ twoFAEnabled: true })
  const user = userEvent.setup()
  await reviewTransferToBob(user)
  const openConfirmation = screen.getByRole('button', {
    name: 'Confirm balance transfer',
  })
  expect(openConfirmation).toBeDisabled()

  await user.type(
    screen.getByLabelText('Two-factor authentication code'),
    '123456'
  )
  await user.click(openConfirmation)

  const confirmation = await screen.findByRole('alertdialog')
  expect(confirmation).toHaveTextContent('@bob')
  expect(confirmation).toHaveTextContent('User ID: 42')
  expect(view.post).not.toHaveBeenCalledWith(
    CONFIRM_URL,
    expect.anything(),
    expect.anything()
  )

  await user.click(screen.getByRole('button', { name: 'Confirm transfer' }))

  await waitFor(() =>
    expect(view.post).toHaveBeenCalledWith(
      CONFIRM_URL,
      {
        quote_token: 'review-1',
        login_password: 'login-pw',
        payment_password: 'pay-pw',
        two_fa_code: '123456',
      },
      { singleUseAuthorization: true }
    )
  )
  await waitFor(() => expect(view.onTransferred).toHaveBeenCalled())
  expect(view.onOpenChange).toHaveBeenCalledWith(false)
})

it('clears the passwords and prepares a new review after a rejected transfer', async () => {
  const view = openTransferDialog({
    twoFAEnabled: false,
    confirm: () => Promise.reject(new Error('rejected')),
  })
  const user = userEvent.setup()
  await reviewTransferToBob(user)
  await user.click(
    screen.getByRole('button', { name: 'Confirm balance transfer' })
  )

  await user.click(
    await screen.findByRole('button', { name: 'Confirm transfer' })
  )

  await waitFor(() =>
    expect(
      view.post.mock.calls.filter((call) => call[0] === QUOTE_URL)
    ).toHaveLength(2)
  )
  await waitFor(() =>
    expect(screen.getByLabelText('Login password')).toHaveValue('')
  )
  expect(screen.getByLabelText('Payment password')).toHaveValue('')
  expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
  expect(view.onTransferred).not.toHaveBeenCalled()
})
