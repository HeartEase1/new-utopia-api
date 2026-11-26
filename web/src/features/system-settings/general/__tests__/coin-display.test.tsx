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
  createMemoryHistory,
  createRootRoute,
  createRouter,
  RouterProvider,
} from '@tanstack/react-router'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { useState } from 'react'
import { expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { SettingsPageProvider } from '../../components/settings-page-context'
import { PricingSection } from '../pricing-section'

function Fixture() {
  const [container, setContainer] = useState<HTMLDivElement | null>(null)
  return (
    <>
      <div ref={setContainer} />
      <SettingsPageProvider actionsContainer={container}>
        <PricingSection
          defaultValues={{
            QuotaPerUnit: 500000,
            USDExchangeRate: 7.2,
            DisplayInCurrencyEnabled: true,
            DisplayTokenStatEnabled: true,
            general_setting: {
              quota_display_type: 'USD',
              custom_currency_symbol: '¤',
              custom_currency_exchange_rate: 8,
            },
          }}
        />
      </SettingsPageProvider>
    </>
  )
}

test('coin preset saves the symbol and custom display mode without changing exchange rates', async () => {
  const put = vi
    .spyOn(api, 'put')
    .mockResolvedValue({ data: { success: true } })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  const router = createRouter({
    routeTree: createRootRoute({ component: Fixture }),
    history: createMemoryHistory({ initialEntries: ['/'] }),
  })
  render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
  fireEvent.click(
    await screen.findByRole('button', { name: 'Use Utopia coins' })
  )
  expect(
    screen.getByRole('textbox', { name: 'Custom Currency Symbol' })
  ).toHaveValue('🪙')
  expect(screen.getByRole('spinbutton', { name: 'Units per USD' })).toHaveValue(
    8
  )
  expect(
    screen.getByRole('spinbutton', { name: 'USD Exchange Rate' })
  ).toHaveValue(7.2)
  fireEvent.click(screen.getByRole('button', { name: 'Save Changes' }))
  await waitFor(() =>
    expect(put).toHaveBeenCalledWith('/api/option/', {
      key: 'general_setting.quota_display_type',
      value: 'CUSTOM',
    })
  )
  expect(put).toHaveBeenCalledWith('/api/option/', {
    key: 'general_setting.custom_currency_symbol',
    value: '🪙',
  })
  expect(
    put.mock.calls.map((call) => (call[1] as { key: string }).key).sort()
  ).toEqual([
    'general_setting.custom_currency_symbol',
    'general_setting.quota_display_type',
  ])
})
