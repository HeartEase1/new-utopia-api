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
import { afterEach, expect, it, vi } from 'vitest'

import type { WalletTransferRecord } from '@/features/wallet/types'
import { api } from '@/lib/api'

import { TransferHistory } from '../components/transfer-history'

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

// alice (ID 1) sent bob (ID 2) an amount of which a fee was kept.
const sent: WalletTransferRecord = {
  id: 9,
  sender_id: 1,
  sender_username: 'alice',
  sender_display_name: 'Alice Wang',
  recipient_id: 2,
  recipient_username: 'bob',
  recipient_display_name: '',
  amount: 1_000_000,
  fee: 20_000,
  received: 980_000,
  fee_percent: '2.00',
  created_at: '2026-11-25T08:30:00+08:00',
  trade_no: 'WT2026112500000009',
}

// Serves the given records in the backend's paged shape (common.PageInfo).
function renderHistory(props: { currentUserId: number; adminView?: boolean }) {
  const page = { page: 1, page_size: 20, total: 1, items: [sent] }
  vi.spyOn(api, 'get').mockResolvedValue({
    data: {
      success: true,
      data: props.adminView ? { page, total_fee: sent.fee } : page,
    },
  })
  render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <TransferHistory
        currentUserId={props.currentUserId}
        adminView={props.adminView}
      />
    </QueryClientProvider>
  )
}

it('lists the sender’s own transfer with recipient, amount, fee and credited amount', async () => {
  renderHistory({ currentUserId: 1 })

  const record = await screen.findByRole('article')

  expect(record).toHaveTextContent('Transfer out')
  expect(record).toHaveTextContent('alice')
  expect(record).toHaveTextContent('bob')
  expect(record).toHaveTextContent('2.00%')
  expect(within(record).getByText('Transfer amount')).toBeVisible()
  expect(within(record).getByText(/^Transfer fee/)).toBeVisible()
  expect(within(record).getByText('Amount received')).toBeVisible()
})

it('names each party by username only, never by display name', async () => {
  renderHistory({ currentUserId: 2 })

  const record = await screen.findByRole('article')

  expect(record).toHaveTextContent('alice')
  expect(record).not.toHaveTextContent('Alice Wang')
  expect(record).not.toHaveTextContent('@')
})

it('shows the order number of each record', async () => {
  renderHistory({ currentUserId: 1 })

  const record = await screen.findByRole('article')

  expect(record).toHaveTextContent('WT2026112500000009')
  expect(
    within(record).getByRole('button', { name: 'Copy order number' })
  ).toBeVisible()
})

it('looks up the user’s own transfer by its order number', async () => {
  renderHistory({ currentUserId: 1 })
  const user = userEvent.setup()
  await screen.findByRole('article')

  await user.type(
    screen.getByLabelText('Find by order number'),
    'WT2026112500000009'
  )

  await waitFor(() =>
    expect(api.get).toHaveBeenLastCalledWith(
      '/api/user/wallet-transfer/history?p=1&page_size=20&trade_no=WT2026112500000009'
    )
  )
})

it('shows the recipient who did not start the transfer who sent it', async () => {
  renderHistory({ currentUserId: 2 })

  const record = await screen.findByRole('article')

  expect(record).toHaveTextContent('Transfer in')
  expect(record).toHaveTextContent('alice')
})

it('lists every transfer with the fee total for an administrator', async () => {
  renderHistory({ currentUserId: 99, adminView: true })

  const record = await screen.findByRole('article')

  expect(record).toHaveTextContent('alice')
  expect(record).toHaveTextContent('bob')
  expect(
    screen.getByText('Total transfer fees in current results')
  ).toBeVisible()
})
