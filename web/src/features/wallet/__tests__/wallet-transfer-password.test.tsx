/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

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
import { afterEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import { WalletTransferPasswordDialog } from '../components/dialogs/wallet-transfer-password-dialog'

afterEach(() => vi.restoreAllMocks())

it('requires login password and 2FA when resetting a payment password', async () => {
  const get = vi.spyOn(api, 'get').mockResolvedValue({
    data: {
      success: true,
      data: {
        has_login_password: true,
        has_email: true,
        email: 'u***@example.com',
        has_payment_password: true,
        two_fa_enabled: true,
        attempt_count: 0,
        attempt_limit: 100,
        used_quota: 0,
        daily_limit_quota: 100,
        daily_limit_amount: 100,
        display_type: 'USD',
        display_symbol: '$',
      },
    },
  })
  const post = vi.spyOn(api, 'post').mockImplementation(async (url) => {
    if (url === '/api/user/wallet-transfer/password/start') {
      return {
        data: {
          success: true,
          data: {
            flow_token: 'one-time-flow',
            email: 'u***@example.com',
            expires_at: Math.floor(Date.now() / 1000) + 600,
          },
        },
      }
    }
    return { data: { success: true, data: { success: true } } }
  })
  const client = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  })
  const onOpenChange = vi.fn()
  const user = userEvent.setup()

  render(
    <QueryClientProvider client={client}>
      <WalletTransferPasswordDialog open onOpenChange={onOpenChange} />
    </QueryClientProvider>
  )

  await screen.findByText(
    /We will send a one-time code to u\*\*\*@example\.com\./
  )
  await user.click(
    screen.getByRole('button', { name: 'Forgot payment password?' })
  )
  await user.click(
    screen.getByRole('button', { name: 'Send verification code' })
  )

  expect(await screen.findByLabelText('Login password')).toBeVisible()
  expect(screen.getByLabelText('Two-factor authentication code')).toBeVisible()
  expect(post).toHaveBeenCalledWith(
    '/api/user/wallet-transfer/password/start',
    {
      intent: 'reset',
    }
  )

  await user.type(screen.getByLabelText('Email verification code'), '123456')
  await user.type(screen.getByLabelText('Login password'), 'login-secret')
  await user.type(
    screen.getByLabelText('Two-factor authentication code'),
    '654321'
  )
  await user.type(
    screen.getByLabelText('New payment password'),
    'new wallet secure password'
  )
  await user.type(
    screen.getByLabelText('Confirm new payment password'),
    'new wallet secure password'
  )
  await user.click(
    screen.getByRole('button', { name: 'Save payment password' })
  )

  await waitFor(() =>
    expect(post).toHaveBeenCalledWith(
      '/api/user/wallet-transfer/password/complete',
      {
        flow_token: 'one-time-flow',
        code: '123456',
        new_password: 'new wallet secure password',
        login_password: 'login-secret',
        two_fa_code: '654321',
      },
      { singleUseAuthorization: true }
    )
  )
  await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false))
  expect(get).toHaveBeenCalledWith('/api/user/wallet-transfer/status')
})
