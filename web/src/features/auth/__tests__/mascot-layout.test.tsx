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
import { afterEach, beforeEach, expect, it } from 'vitest'

import { ThemeCustomizationProvider } from '@/context/theme-customization-provider'
import { THEME_STORAGE_KEYS } from '@/lib/theme-storage'

import { AuthLayout } from '../auth-layout'

async function renderLayout(preset: string | null) {
  if (preset) localStorage.setItem(THEME_STORAGE_KEYS.preset, preset)
  const router = createRouter({
    routeTree: createRootRoute({
      component: () => (
        <ThemeCustomizationProvider>
          <AuthLayout>
            <button type='submit'>Sign in</button>
          </AuthLayout>
        </ThemeCustomizationProvider>
      ),
    }),
    history: createMemoryHistory({ initialEntries: ['/'] }),
  })
  await router.load()
  render(
    <QueryClientProvider client={new QueryClient()}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
}

beforeEach(() => {
  localStorage.clear()
})

afterEach(() => {
  cleanup()
  localStorage.clear()
  document.body.removeAttribute('data-theme-preset')
})

it('shows the character greeting beside the form under a character preset', async () => {
  await renderLayout('mascot-grok')

  const panel = screen.getByRole('complementary', {
    name: "Grok's midnight castle",
  })
  expect(panel).toHaveTextContent(
    'Say it straight. Rules? I only care who wins.'
  )
  expect(screen.getByRole('button', { name: 'Sign in' })).toBeInTheDocument()
})

it('credits the artwork in the panel and next to the form', async () => {
  await renderLayout('mascot-deepseek')

  const credits = screen.getAllByRole('link', { name: /CC BY-NC-SA 4\.0/ })
  expect(credits).toHaveLength(2)
  for (const credit of credits) {
    expect(credit).toHaveAttribute(
      'href',
      'https://github.com/lokicorvus/crosspet'
    )
  }
  const panel = screen.getByRole('complementary')
  expect(within(panel).getByRole('link')).toBe(credits[0])
})

it('allows the form to grow beyond a short viewport without clipping it', async () => {
  await renderLayout('mascot-claude')

  const layout = screen.getByRole('complementary').parentElement
  expect(layout).toHaveClass('min-h-svh', 'overflow-x-clip')
  expect(layout).not.toHaveClass('h-svh', 'overflow-hidden')
})

it('keeps the character artwork out of the accessibility tree', async () => {
  await renderLayout('mascot-claude')

  // Only named images (the site logo, once loaded) may be exposed.
  for (const image of screen.queryAllByRole('img')) {
    expect(image).toHaveAccessibleName()
  }
  expect(document.querySelectorAll('img[aria-hidden="true"]')).toHaveLength(2)
})

it.each([null, 'rose-garden'])(
  'leaves the auth page undecorated when the preset is %s',
  async (preset) => {
    await renderLayout(preset)

    expect(screen.queryByRole('complementary')).not.toBeInTheDocument()
    expect(
      screen.queryByRole('link', { name: /CrossPet/ })
    ).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Sign in' })).toBeInTheDocument()
  }
)
