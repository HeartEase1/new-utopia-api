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
  cleanup,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { AxiosError } from 'axios'
import { afterEach, expect, it, vi } from 'vitest'

import type { WalletTransferStatus } from '@/features/wallet/types'
import { api } from '@/lib/api'

import { TransferForm } from '../components/transfer-form'

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

const QUOTE_URL = '/api/user/wallet-transfer/quote'
const CONFIRM_URL = '/api/user/wallet-transfer/confirm'

function senderStatus(
  values: Partial<WalletTransferStatus> = {}
): WalletTransferStatus {
  return {
    has_login_password: true,
    has_email: true,
    email_exempt: false,
    limits_exempt: false,
    email: 'u***@example.com',
    has_payment_password: true,
    two_fa_enabled: false,
    attempt_count: 0,
    attempt_limit: 10,
    fee_percent: '0.00',
    used_quota: 0,
    daily_limit_quota: 50_000_000,
    daily_limit_amount: 100,
    min_quota: 500_000,
    display_type: 'USD',
    display_symbol: '$',
    ...values,
  }
}

// Mounts the form for a sender whose search finds "bob" (ID 42) and whose every
// review is answered for him; `confirm` decides how a submission ends.
function renderForm(
  status: WalletTransferStatus,
  confirm?: () => Promise<unknown>
) {
  vi.spyOn(api, 'get').mockResolvedValue({
    data: {
      success: true,
      data: [{ id: 42, username: 'bob', display_name: '' }],
    },
  })
  const post = vi.spyOn(api, 'post').mockImplementation(async (url, body) => {
    if (url === CONFIRM_URL) {
      if (confirm) return confirm()
      return {
        data: {
          success: true,
          data: {
            id: 7,
            recipient_id: 42,
            recipient_username: 'bob',
            amount: 1_000_000,
            fee: 0,
            received: 1_000_000,
          },
        },
      }
    }
    const amount = (body as { amount: number }).amount
    return {
      data: {
        success: true,
        data: {
          quote_token: 'review-1',
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
  })
  const onAttempted = vi.fn()
  render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <TransferForm
        status={status}
        availableQuota={10_000_000}
        onAttempted={onAttempted}
      />
    </QueryClientProvider>
  )
  return { post, onAttempted }
}

// Picks bob and enters an amount.
async function fillTransferToBob(user: ReturnType<typeof userEvent.setup>) {
  await user.type(screen.getByLabelText('Recipient username or user ID'), 'bob')
  await user.click(await screen.findByRole('button', { name: /bob/ }))
  await user.type(screen.getByLabelText('Transfer amount'), '2')
}

it('keeps the transfer button unavailable until a payment password is set', async () => {
  renderForm(senderStatus({ has_payment_password: false }))
  const user = userEvent.setup()

  await fillTransferToBob(user)

  expect(
    screen.getByRole('button', { name: 'Transfer balance' })
  ).toBeDisabled()
})

it('blocks an ordinary sender who has used every attempt but not the super administrator', async () => {
  const exhausted = { attempt_count: 10, used_quota: 50_000_000 }
  renderForm(senderStatus(exhausted))
  const user = userEvent.setup()
  await fillTransferToBob(user)
  expect(
    screen.getByRole('button', { name: 'Transfer balance' })
  ).toBeDisabled()
  cleanup()

  renderForm(senderStatus({ ...exhausted, limits_exempt: true }))
  await fillTransferToBob(user)

  expect(screen.getByRole('button', { name: 'Transfer balance' })).toBeEnabled()
})

it('refuses an amount below the minimum of one unit', async () => {
  renderForm(senderStatus())
  const user = userEvent.setup()
  await user.type(screen.getByLabelText('Recipient username or user ID'), 'bob')
  await user.click(await screen.findByRole('button', { name: /bob/ }))

  await user.type(screen.getByLabelText('Transfer amount'), '0.5')

  expect(screen.getByRole('alert')).toHaveTextContent(
    'The minimum transfer amount is'
  )
  expect(
    screen.getByRole('button', { name: 'Transfer balance' })
  ).toBeDisabled()
})

it('shows the recipient, amounts and the irreversibility warning before asking for passwords', async () => {
  const view = renderForm(senderStatus())
  const user = userEvent.setup()
  await fillTransferToBob(user)

  await user.click(screen.getByRole('button', { name: 'Transfer balance' }))

  const dialog = await screen.findByRole('dialog')
  expect(dialog).toHaveTextContent('bob')
  expect(dialog).toHaveTextContent('42')
  expect(within(dialog).getByText('A transfer cannot be undone')).toBeVisible()
  expect(
    within(dialog).queryByLabelText('Payment password')
  ).not.toBeInTheDocument()

  await user.click(within(dialog).getByRole('button', { name: 'Back to edit' }))

  await waitFor(() =>
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  )
  expect(
    view.post.mock.calls.filter((call) => call[0] === CONFIRM_URL)
  ).toHaveLength(0)
})

it('transfers after the details are confirmed and the passwords and 2FA code are entered', async () => {
  const view = renderForm(senderStatus({ two_fa_enabled: true }))
  const user = userEvent.setup()
  await fillTransferToBob(user)
  await user.click(screen.getByRole('button', { name: 'Transfer balance' }))
  const dialog = await screen.findByRole('dialog')
  await user.click(
    within(dialog).getByRole('button', { name: 'Correct, continue' })
  )
  const submit = within(dialog).getByRole('button', {
    name: 'Confirm transfer',
  })
  await user.type(within(dialog).getByLabelText('Payment password'), 'pay-pw')
  await user.type(within(dialog).getByLabelText('Login password'), 'login-pw')
  expect(submit).toBeDisabled()

  await user.type(
    within(dialog).getByLabelText('Two-factor authentication code'),
    '123456'
  )
  await user.click(submit)

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
  expect(await screen.findByText('Transfer completed')).toBeVisible()
  expect(view.onAttempted).toHaveBeenCalled()

  await user.click(
    screen.getByRole('button', { name: 'Make another transfer' })
  )

  expect(screen.getByLabelText('Recipient username or user ID')).toHaveValue('')
})

it('keeps the same review after a network failure and finds the completed transfer with it', async () => {
  let confirmCalls = 0
  const view = renderForm(senderStatus(), async () => {
    confirmCalls += 1
    if (confirmCalls === 1) {
      // The response never arrived: the server may or may not have transferred.
      throw new AxiosError('Network Error', 'ERR_NETWORK')
    }
    return {
      data: {
        success: true,
        data: {
          id: 7,
          trade_no: 'WT2026112500000007',
          recipient_id: 42,
          recipient_username: 'bob',
          amount: 1_000_000,
          fee: 0,
          received: 1_000_000,
        },
      },
    }
  })
  const user = userEvent.setup()
  await fillTransferToBob(user)
  await user.click(screen.getByRole('button', { name: 'Transfer balance' }))
  const dialog = await screen.findByRole('dialog')
  await user.click(
    within(dialog).getByRole('button', { name: 'Correct, continue' })
  )
  await user.type(within(dialog).getByLabelText('Payment password'), 'pay-pw')
  await user.type(within(dialog).getByLabelText('Login password'), 'login-pw')
  await user.click(
    within(dialog).getByRole('button', { name: 'Confirm transfer' })
  )

  const check = await within(dialog).findByRole('button', {
    name: 'Check result',
  })
  expect(dialog).toHaveTextContent('Transfer result not confirmed')

  await user.click(check)

  expect(await screen.findByText('Transfer completed')).toBeVisible()
  const confirms = view.post.mock.calls.filter(
    (call) => call[0] === CONFIRM_URL
  )
  expect(confirms).toHaveLength(2)
  expect(confirms[1][1]).toEqual(confirms[0][1])
  expect(
    view.post.mock.calls.filter((call) => call[0] === QUOTE_URL)
  ).toHaveLength(1)
})

it('closes the dialog and keeps recipient and amount after a rejected transfer', async () => {
  const view = renderForm(senderStatus(), () =>
    Promise.reject(new Error('rejected'))
  )
  const user = userEvent.setup()
  await fillTransferToBob(user)
  await user.click(screen.getByRole('button', { name: 'Transfer balance' }))
  const dialog = await screen.findByRole('dialog')
  await user.click(
    within(dialog).getByRole('button', { name: 'Correct, continue' })
  )
  await user.type(within(dialog).getByLabelText('Payment password'), 'wrong')
  await user.type(within(dialog).getByLabelText('Login password'), 'login-pw')

  await user.click(
    within(dialog).getByRole('button', { name: 'Confirm transfer' })
  )

  await waitFor(() =>
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  )
  expect(screen.getByLabelText('Transfer amount')).toHaveValue(2)
  expect(screen.getByText('bob')).toBeVisible()
  expect(screen.queryByText('Transfer completed')).not.toBeInTheDocument()
  expect(view.onAttempted).toHaveBeenCalled()
  expect(
    view.post.mock.calls.filter((call) => call[0] === QUOTE_URL)
  ).toHaveLength(1)
})
