import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { afterEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import { SettingsPageProvider } from '../../components/settings-page-context'
import {
  WalletDisplaySection,
  type WalletDisplayDefaults,
} from '../wallet-display-section'

const defaults: WalletDisplayDefaults = {
  announcement: '',
  redemptionStores: '',
  showSubscriptionPlans: true,
}

function WalletDisplayFixture(props: { values: WalletDisplayDefaults }) {
  const [actions, setActions] = useState<HTMLDivElement | null>(null)
  return (
    <SettingsPageProvider actionsContainer={actions}>
      <div ref={setActions} />
      <WalletDisplaySection defaultValues={props.values} />
    </SettingsPageProvider>
  )
}

function renderSection(values: WalletDisplayDefaults = defaults) {
  render(
    <QueryClientProvider
      client={
        new QueryClient({
          defaultOptions: { queries: { retry: false } },
        })
      }
    >
      <WalletDisplayFixture values={values} />
    </QueryClientProvider>
  )
  return userEvent.setup()
}

afterEach(() => vi.restoreAllMocks())

it('saves the announcement, every store, and the subscription toggle separately', async () => {
  const request = vi
    .spyOn(api, 'put')
    .mockResolvedValue({ data: { success: true } })
  const user = renderSection()

  await user.type(
    screen.getByLabelText('Wallet announcement'),
    'Check the amount before paying.'
  )
  await user.click(screen.getByRole('button', { name: 'Add store' }))
  await user.type(screen.getByLabelText('Purchase button name'), 'Utopia Store')
  await user.type(
    screen.getByLabelText('Purchase link'),
    'https://store.example/codes'
  )
  await user.type(screen.getByLabelText('Fee description'), '2%')
  await user.click(screen.getByRole('button', { name: 'Add' }))
  expect(screen.getByText('Utopia Store')).toBeVisible()

  await user.click(screen.getByRole('button', { name: 'Add store' }))
  await user.type(screen.getByLabelText('Purchase button name'), 'Backup Store')
  await user.type(
    screen.getByLabelText('Purchase link'),
    'https://backup.example/codes'
  )
  await user.click(screen.getByRole('button', { name: 'Add' }))
  expect(screen.getByText('Backup Store')).toBeVisible()

  await user.click(
    screen.getByRole('switch', { name: 'Show subscription plans' })
  )
  await user.click(
    screen.getByRole('button', { name: 'Save wallet display settings' })
  )

  await waitFor(() => expect(request).toHaveBeenCalledTimes(3))
  expect(request.mock.calls.map((call) => call[1])).toEqual([
    {
      key: 'payment_setting.wallet_announcement',
      value: 'Check the amount before paying.',
    },
    {
      key: 'payment_setting.redemption_purchase',
      value: JSON.stringify(
        [
          {
            name: 'Utopia Store',
            url: 'https://store.example/codes',
            fee: '2%',
            payment_methods: '',
          },
          {
            name: 'Backup Store',
            url: 'https://backup.example/codes',
            fee: '',
            payment_methods: '',
          },
        ],
        null,
        2
      ),
    },
    { key: 'payment_setting.show_subscription_plans', value: 'false' },
  ])
})

it('edits and removes a configured store', async () => {
  const request = vi
    .spyOn(api, 'put')
    .mockResolvedValue({ data: { success: true } })
  const user = renderSection({
    ...defaults,
    redemptionStores: JSON.stringify([
      {
        name: 'Store A',
        url: 'https://a.example',
        fee: '',
        payment_methods: '',
      },
      { name: 'Store B', url: '', fee: '', payment_methods: '' },
    ]),
  })

  expect(screen.getByText('Hidden: no purchase link')).toBeVisible()

  await user.click(screen.getAllByRole('button', { name: 'Edit' })[0])
  const nameField = screen.getByLabelText('Purchase button name')
  await user.clear(nameField)
  await user.type(nameField, 'Store A renamed')
  await user.click(screen.getByRole('button', { name: 'Update' }))
  expect(screen.getByText('Store A renamed')).toBeVisible()

  await user.click(screen.getAllByRole('button', { name: 'Open menu' })[1])
  await user.click(screen.getByRole('menuitem', { name: 'Delete' }))
  expect(screen.queryByText('Store B')).not.toBeInTheDocument()

  await user.click(
    screen.getByRole('button', { name: 'Save wallet display settings' })
  )
  await waitFor(() => expect(request).toHaveBeenCalledTimes(1))
  expect(request.mock.calls[0][1]).toEqual({
    key: 'payment_setting.redemption_purchase',
    value: JSON.stringify(
      [
        {
          name: 'Store A renamed',
          url: 'https://a.example',
          fee: '',
          payment_methods: '',
        },
      ],
      null,
      2
    ),
  })
})

it('rejects an unsafe purchase link before saving', async () => {
  const request = vi
    .spyOn(api, 'put')
    .mockResolvedValue({ data: { success: true } })
  const user = renderSection()

  await user.click(screen.getByRole('button', { name: 'Add store' }))
  await user.type(screen.getByLabelText('Purchase button name'), 'Store')
  await user.type(screen.getByLabelText('Purchase link'), 'javascript:alert(1)')
  await user.click(screen.getByRole('button', { name: 'Add' }))

  expect(
    screen.getByText(
      'Purchase link must be an HTTP or HTTPS URL without credentials'
    )
  ).toBeVisible()
  expect(request).not.toHaveBeenCalled()
})
