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

import type { AlipayF2FSettings } from '../alipay-f2f-api'
import { AlipayF2FSettingsSection } from '../alipay-f2f-settings-section'

const i18n = createInstance()
const settings: AlipayF2FSettings = {
  enabled: true,
  sandbox: false,
  app_id: '2026000000000000',
  seller_id: '2088000000000000',
  notify_url: 'https://pay.example/api/alipay-f2f/notify',
  private_key_configured: true,
  alipay_public_key_configured: true,
  from_environment: false,
  revision: 'revision-1',
  key_storage: 'file',
  private_key_unavailable: false,
  encryption_available: true,
}

beforeEach(async () => {
  await i18n.use(initReactI18next).init({
    lng: 'en',
    fallbackLng: false,
    resources: { en: { translation: {} } },
    interpolation: { escapeValue: false },
  })
})

function show() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  })
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={client}>
        <AlipayF2FSettingsSection />
      </QueryClientProvider>
    </I18nextProvider>
  )
}

it('keeps saved keys blank and saves a disabled gateway using the existing keys', async () => {
  vi.spyOn(api, 'get').mockResolvedValue({
    data: { success: true, data: settings },
  })
  const put = vi.spyOn(api, 'put').mockResolvedValue({
    data: {
      success: true,
      data: { ...settings, enabled: false, revision: 'revision-2' },
    },
  })
  show()
  expect(await screen.findByLabelText('Alipay app ID')).toHaveValue(
    settings.app_id
  )
  expect(screen.getByLabelText('Alipay application private key')).toHaveValue(
    ''
  )
  expect(screen.getByLabelText('Alipay public key')).toHaveValue('')
  expect(screen.getByLabelText('Payment callback URL')).toHaveValue(
    settings.notify_url
  )
  const user = userEvent.setup()
  const toggle = screen.getByRole('switch', {
    name: 'Enable Alipay F2F top-ups',
  })
  toggle.focus()
  await user.keyboard(' ')
  expect(toggle).not.toBeChecked()
  await user.click(screen.getByRole('button', { name: 'Save Alipay settings' }))
  await waitFor(() =>
    expect(put).toHaveBeenCalledWith('/api/option/alipay-f2f', {
      enabled: false,
      sandbox: false,
      app_id: settings.app_id,
      seller_id: settings.seller_id,
      notify_url: settings.notify_url,
      key_storage: 'file',
      private_key: '',
      alipay_public_key: '',
      revision: settings.revision,
    })
  )
})

it('saves encrypted storage when the administrator selects it for an unencrypted legacy key', async () => {
  vi.spyOn(api, 'get').mockResolvedValue({
    data: { success: true, data: { ...settings, key_storage: 'database' } },
  })
  const put = vi.spyOn(api, 'put').mockResolvedValue({
    data: {
      success: true,
      data: { ...settings, key_storage: 'encrypted', revision: 'revision-2' },
    },
  })
  show()
  const file = await screen.findByRole('radio', { name: 'Server file' })
  expect(file).toBeChecked()
  expect(
    screen.getByText(
      'The private key is currently stored unencrypted in the database. Save to move it to the selected storage.'
    )
  ).toBeInTheDocument()
  const user = userEvent.setup()
  await user.click(screen.getByRole('radio', { name: 'Encrypted in database' }))
  await user.click(screen.getByRole('button', { name: 'Save Alipay settings' }))
  await waitFor(() =>
    expect(put).toHaveBeenCalledWith(
      '/api/option/alipay-f2f',
      expect.objectContaining({ key_storage: 'encrypted', private_key: '' })
    )
  )
})

it('disables encrypted storage without CRYPTO_SECRET and asks for the key again when it is unreadable', async () => {
  vi.spyOn(api, 'get').mockResolvedValue({
    data: {
      success: true,
      data: {
        ...settings,
        encryption_available: false,
        private_key_configured: false,
        private_key_unavailable: true,
      },
    },
  })
  show()
  expect(
    await screen.findByRole('radio', { name: 'Encrypted in database' })
  ).toHaveAttribute('aria-disabled', 'true')
  expect(
    screen.getByText(
      'Set the CRYPTO_SECRET environment variable to enable encrypted storage.'
    )
  ).toBeInTheDocument()
  expect(
    screen.getByText(
      'This server cannot read the saved private key. Enter the private key again and save.'
    )
  ).toBeInTheDocument()
})

it('clears newly entered keys only after a successful save and retains edits on failure', async () => {
  vi.spyOn(api, 'get').mockResolvedValue({
    data: { success: true, data: settings },
  })
  const put = vi
    .spyOn(api, 'put')
    .mockResolvedValueOnce({
      data: { success: false, message: 'Failed to save Alipay settings.' },
    })
    .mockResolvedValueOnce({
      data: { success: true, data: { ...settings, revision: 'revision-2' } },
    })
  show()
  const privateKey = await screen.findByLabelText(
    'Alipay application private key'
  )
  const user = userEvent.setup()
  await user.type(privateKey, 'new-private-key')
  await user.click(screen.getByRole('button', { name: 'Save Alipay settings' }))
  await waitFor(() => expect(put).toHaveBeenCalledTimes(1))
  expect(privateKey).toHaveValue('new-private-key')
  await user.click(screen.getByRole('button', { name: 'Save Alipay settings' }))
  await waitFor(() =>
    expect(screen.getByLabelText('Alipay application private key')).toHaveValue(
      ''
    )
  )
})

it('checks credentials without saving and rejects an HTTP callback before sending', async () => {
  vi.spyOn(api, 'get').mockResolvedValue({
    data: { success: true, data: settings },
  })
  const post = vi
    .spyOn(api, 'post')
    .mockResolvedValue({ data: { success: true } })
  const put = vi.spyOn(api, 'put')
  show()
  const user = userEvent.setup()
  await user.click(
    await screen.findByRole('button', { name: 'Check connection' })
  )
  await waitFor(() =>
    expect(post).toHaveBeenCalledWith(
      '/api/option/alipay-f2f/check',
      expect.objectContaining({ revision: 'revision-1', private_key: '' })
    )
  )
  expect(put).not.toHaveBeenCalled()
  const callback = screen.getByLabelText('Payment callback URL')
  await user.clear(callback)
  await user.type(callback, 'http://pay.example/api/alipay-f2f/notify')
  await user.click(screen.getByRole('button', { name: 'Save Alipay settings' }))
  await waitFor(() => expect(callback).toHaveAttribute('aria-invalid', 'true'))
  expect(put).not.toHaveBeenCalled()
})

it('offers retry when loading fails and keeps controls unavailable until loaded', async () => {
  vi.spyOn(api, 'get')
    .mockRejectedValueOnce(new Error('offline'))
    .mockResolvedValueOnce({ data: { success: true, data: settings } })
  show()
  expect(
    screen.queryByRole('button', { name: 'Save Alipay settings' })
  ).not.toBeInTheDocument()
  const user = userEvent.setup()
  await user.click(await screen.findByRole('button', { name: 'Retry' }))
  expect(await screen.findByLabelText('Alipay app ID')).toHaveValue(
    settings.app_id
  )
})
