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
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { SettingsPageProvider } from '../../components/settings-page-context'
import { ThemeSection } from '../theme-section'

function Fixture() {
  const [container, setContainer] = useState<HTMLDivElement | null>(null)
  return (
    <>
      <div ref={setContainer} />
      <SettingsPageProvider actionsContainer={container}>
        <ThemeSection defaultPreset='default' />
      </SettingsPageProvider>
    </>
  )
}

async function renderSettings() {
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
  await screen.findByRole('radiogroup', { name: 'Default theme' })
}

test('keyboard theme selection saves the site default and keeps selection on success', async () => {
  const put = vi
    .spyOn(api, 'put')
    .mockResolvedValue({ data: { success: true } })
  await renderSettings()
  const user = userEvent.setup()
  const defaults = screen.getByRole('radio', { name: 'preset.default' })
  expect(defaults).toBeChecked()
  defaults.focus()
  await user.keyboard('{ArrowRight}')
  const utopia = screen.getByRole('radio', { name: 'preset.new-utopia' })
  expect(utopia).toBeChecked()
  await user.click(screen.getByRole('button', { name: 'Save Changes' }))
  await waitFor(() =>
    expect(put).toHaveBeenCalledWith('/api/option/', {
      key: 'theme_setting.default_preset',
      value: 'new-utopia',
    })
  )
  expect(utopia).toBeChecked()
  await waitFor(() =>
    expect(screen.getByRole('button', { name: 'Reset' })).toBeDisabled()
  )
})

test('failed theme save keeps the draft available to reset', async () => {
  vi.spyOn(api, 'put').mockResolvedValue({
    data: { success: false, message: 'Cannot save theme' },
  })
  await renderSettings()
  const user = userEvent.setup()
  await user.click(screen.getByRole('radio', { name: 'preset.new-utopia' }))
  await user.click(screen.getByRole('button', { name: 'Save Changes' }))
  await waitFor(() =>
    expect(screen.getByRole('button', { name: 'Save Changes' })).toBeEnabled()
  )
  expect(screen.getByRole('radio', { name: 'preset.new-utopia' })).toBeChecked()
  const reset = screen.getByRole('button', { name: 'Reset' })
  expect(reset).toBeEnabled()
  await user.click(reset)
  expect(screen.getByRole('radio', { name: 'preset.default' })).toBeChecked()
})
