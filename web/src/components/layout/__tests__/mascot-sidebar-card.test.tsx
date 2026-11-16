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
import { cleanup, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'

import { SidebarProvider, SidebarTrigger } from '@/components/ui/sidebar'
import { LayoutProvider } from '@/context/layout-provider'
import { ThemeCustomizationProvider } from '@/context/theme-customization-provider'
import { ROLE } from '@/lib/roles'
import { STATUS_QUERY_KEY } from '@/lib/status-query'
import { THEME_STORAGE_KEYS } from '@/lib/theme-storage'
import { useAuthStore } from '@/stores/auth-store'

import { AppSidebar } from '../components/app-sidebar'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  useAuthStore.getState().auth.reset()
  localStorage.clear()
  document.body.removeAttribute('data-theme-preset')
})

async function renderSidebar(preset: string | null, width: number) {
  vi.stubGlobal('innerWidth', width)
  if (preset) localStorage.setItem(THEME_STORAGE_KEYS.preset, preset)
  useAuthStore
    .getState()
    .auth.setUser({ id: 1, username: 'alice', role: ROLE.USER })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  client.setQueryData(STATUS_QUERY_KEY, {})
  const router = createRouter({
    routeTree: createRootRoute({
      component: () => (
        <ThemeCustomizationProvider>
          <LayoutProvider>
            <SidebarProvider>
              <SidebarTrigger />
              <AppSidebar />
            </SidebarProvider>
          </LayoutProvider>
        </ThemeCustomizationProvider>
      ),
    }),
    history: createMemoryHistory({ initialEntries: ['/dashboard/overview'] }),
  })
  await router.load()
  render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
  if (width < 768) {
    await userEvent.click(
      await screen.findByRole('button', { name: 'Toggle Sidebar' })
    )
    await screen.findByRole('dialog')
  }
}

it('shows the character card with its art credit in the mobile drawer', async () => {
  await renderSidebar('mascot-claude', 390)

  const drawer = screen.getByRole('dialog')
  expect(
    within(drawer).getByText('A good answer starts with reading the question.')
  ).toBeVisible()
  expect(
    within(drawer).getByRole('link', { name: /CrossPet/ })
  ).toHaveAttribute('href', 'https://github.com/lokicorvus/crosspet')
})

it('shows the character card in the desktop sidebar', async () => {
  await renderSidebar('mascot-deepseek', 1280)

  expect(await screen.findByText('Deep thoughts, served warm.')).toBeVisible()
})

it('adds no card under a preset without a character', async () => {
  await renderSidebar('rose-garden', 1280)

  expect(await screen.findByRole('link', { name: 'API Keys' })).toBeVisible()
  expect(
    screen.queryByRole('link', { name: /CrossPet/ })
  ).not.toBeInTheDocument()
})
