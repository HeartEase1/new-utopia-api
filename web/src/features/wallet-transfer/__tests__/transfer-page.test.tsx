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
import { cleanup, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactNode } from 'react'
import { afterEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import { WalletTransfer } from '..'

vi.mock('@tanstack/react-router', () => ({
  Link: (props: { to: string; children?: ReactNode }) => (
    <a href={props.to}>{props.children}</a>
  ),
}))

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  useAuthStore.getState().auth.reset()
})

// The page of a super administrator who has not set a payment password yet.
function renderPage() {
  vi.spyOn(api, 'get').mockImplementation(async (url: string) => {
    if (url === '/api/user/wallet-transfer/status') {
      return {
        data: {
          success: true,
          data: {
            has_login_password: true,
            has_email: true,
            email_exempt: true,
            limits_exempt: true,
            email: 'r***@example.com',
            has_payment_password: false,
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
          },
        },
      }
    }
    if (url === '/api/user/self') {
      return { data: { success: true, data: { id: 1, quota: 10_000_000 } } }
    }
    return { data: { success: true, data: { items: [], total: 0 } } }
  })
  render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <WalletTransfer />
    </QueryClientProvider>
  )
}

it('opens the payment password dialog when the sender has none yet', async () => {
  renderPage()
  const user = userEvent.setup()
  const [setPassword] = await screen.findAllByRole('button', {
    name: 'Set payment password',
  })

  await user.click(setPassword)

  const dialog = await screen.findByRole('dialog')
  expect(within(dialog).getByRole('button', { name: 'Continue' })).toBeEnabled()
})

it('opens site-wide reconciliation for an administrator', async () => {
  useAuthStore
    .getState()
    .auth.setUser({ id: 1, username: 'root', role: ROLE.SUPER_ADMIN })
  renderPage()
  const user = userEvent.setup()

  await user.click(
    await screen.findByRole('button', { name: 'Site-wide reconciliation' })
  )

  expect(await screen.findByRole('dialog')).toHaveTextContent(
    'Wallet transfer reconciliation'
  )
})
