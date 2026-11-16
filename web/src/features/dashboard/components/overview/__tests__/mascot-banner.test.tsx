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
import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'

import { ThemeCustomizationProvider } from '@/context/theme-customization-provider'
import { THEME_STORAGE_KEYS } from '@/lib/theme-storage'
import { useAuthStore } from '@/stores/auth-store'

import { MascotBanner } from '../mascot-banner'

function renderBanner(preset: string | null, username = 'alice') {
  if (preset) localStorage.setItem(THEME_STORAGE_KEYS.preset, preset)
  useAuthStore.getState().auth.setUser({ id: 1, username, role: 1 })
  return render(
    <ThemeCustomizationProvider>
      <MascotBanner />
    </ThemeCustomizationProvider>
  )
}

beforeEach(() => {
  localStorage.clear()
})

afterEach(() => {
  cleanup()
  useAuthStore.setState(useAuthStore.getInitialState(), true)
  localStorage.clear()
  document.body.removeAttribute('data-theme-preset')
})

describe('mascot welcome banner', () => {
  it.each([
    ['mascot-claude', "Claude's reading room"],
    ['mascot-deepseek', "DeepSeek's seaside diner"],
    ['mascot-gpt', "GPT's castle in the clouds"],
  ])('greets the signed-in user under the %s preset', (preset, regionName) => {
    renderBanner(preset)

    const banner = screen.getByRole('region', { name: regionName })
    expect(banner).toHaveTextContent('Welcome back!')
    expect(banner).toHaveTextContent('alice')
  })

  it('renders nothing under a preset without a character', () => {
    const view = renderBanner('rose-garden')

    expect(view.container).toBeEmptyDOMElement()
  })

  it('renders nothing when no preset has been chosen', () => {
    const view = renderBanner(null)

    expect(view.container).toBeEmptyDOMElement()
  })

  it('credits the artwork with a link to its source and license', () => {
    renderBanner('mascot-deepseek')

    const credit = screen.getByRole('link', { name: /CC BY-NC-SA 4\.0/ })
    expect(credit).toHaveAttribute(
      'href',
      'https://github.com/lokicorvus/crosspet'
    )
    expect(credit).toHaveTextContent('ZipZipPipe')
  })

  it('keeps the artwork out of the accessibility tree and adds no heading', () => {
    renderBanner('mascot-gpt')

    expect(screen.queryByRole('img')).not.toBeInTheDocument()
    expect(screen.queryByRole('heading')).not.toBeInTheDocument()
  })

  it('shows a very long display name in full text for assistive tech', () => {
    const longName = 'a-very-long-username-'.repeat(6)
    renderBanner('mascot-claude', longName)

    expect(screen.getByText(longName)).toBeInTheDocument()
  })
})
