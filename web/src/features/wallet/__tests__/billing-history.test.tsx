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
import { render, screen } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'
import {
  DEFAULT_CURRENCY_CONFIG,
  useSystemConfigStore,
} from '@/stores/system-config-store'

import { BillingHistoryDialog } from '../components/dialogs/billing-history-dialog'
import type { TopupRecord } from '../types'

const record: TopupRecord = {
  id: 1,
  user_id: 42,
  amount: 1,
  money: 7,
  trade_no: 'UT-rebate-history',
  payment_method: 'alipay_f2f',
  create_time: 1700000000,
  complete_time: 1700000030,
  status: 'success',
  referral_user_id: 201,
  referral_quota: 62500,
}

const originalConfig = useSystemConfigStore.getState().config

beforeEach(() => {
  useAuthStore.getState().auth.setUser({ id: 2, username: 'admin', role: 10 })
  useSystemConfigStore
    .getState()
    .setConfig({ currency: DEFAULT_CURRENCY_CONFIG })
})

afterEach(() => {
  vi.restoreAllMocks()
  useAuthStore.getState().auth.reset()
  useSystemConfigStore.getState().setConfig(originalConfig)
})

function showHistory(records = [record]) {
  const get = vi.spyOn(api, 'get').mockResolvedValue({
    data: { success: true, data: { items: records, total: records.length } },
  })
  render(<BillingHistoryDialog open onOpenChange={vi.fn()} />)
  return get
}

it('shows the payer, saved rebate recipient, wallet credit and completion time to admins', async () => {
  const get = showHistory()
  await screen.findByText('UT-rebate-history')
  expect(screen.getByText('User ID: 42')).toBeVisible()
  expect(screen.getByText('User ID: 201')).toBeVisible()
  expect(screen.getByText('$0.125')).toBeVisible()
  expect(screen.getByText('Completion Time')).toBeVisible()
  expect(screen.getByText('Payment')).toBeVisible()
  expect(get).toHaveBeenCalledWith('/api/user/topup?p=1&page_size=10')
})

it('marks completed orders without a recorded rebate and leaves pending orders without rebate details', async () => {
  showHistory([
    { ...record, referral_user_id: 0, referral_quota: 0 },
    { ...record, id: 2, trade_no: 'UT-pending', status: 'pending' },
  ])
  expect(await screen.findByText('No rebate recorded')).toBeVisible()
  expect(screen.getAllByText('Referral rebate')).toHaveLength(1)
  expect(screen.queryByText('User ID: 201')).not.toBeInTheDocument()
})

it('preserves a one-unit rebate instead of rounding it to zero', async () => {
  showHistory([{ ...record, referral_quota: 1 }])
  expect(await screen.findByText('$0.000002')).toBeVisible()
})

it('hides admin rebate details in ordinary users’ self-service history even if supplied', async () => {
  useAuthStore.getState().auth.setUser({ id: 42, username: 'payer', role: 1 })
  const get = showHistory()
  await screen.findByText('UT-rebate-history')
  expect(screen.queryByText('Rebate recipient')).not.toBeInTheDocument()
  expect(screen.queryByText('Referral rebate')).not.toBeInTheDocument()
  expect(screen.queryByText('User ID: 201')).not.toBeInTheDocument()
  expect(get).toHaveBeenCalledWith('/api/user/topup/self?p=1&page_size=10')
})
