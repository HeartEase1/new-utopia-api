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
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { THEME_STORAGE_KEYS } from '@/lib/theme-storage'

// The inline script in index.html must apply the theme class before React
// mounts; Dark Reader checks `html.dark` only once while the page loads.
const html = readFileSync(
  resolve(import.meta.dirname, '../../../index.html'),
  'utf8'
)
const inlineScripts = [...html.matchAll(/<script>([\s\S]*?)<\/script>/g)]
const bootScript = inlineScripts[0]?.[1] ?? ''
const presetBootScript = inlineScripts[1]?.[1] ?? ''

function runBootScript() {
  new Function(bootScript)()
}

function runPresetBootScript() {
  new Function(presetBootScript)()
}

function cacheSiteDefaultPreset(preset: string) {
  localStorage.setItem(
    'system-config-storage',
    JSON.stringify({ state: { config: { defaultThemePreset: preset } } })
  )
}

function mockSystemDarkMode(dark: boolean) {
  vi.spyOn(window, 'matchMedia').mockImplementation(
    (query: string) =>
      ({
        matches: dark && query === '(prefers-color-scheme: dark)',
        media: query,
      }) as MediaQueryList
  )
}

beforeEach(() => {
  localStorage.clear()
})

afterEach(() => {
  vi.restoreAllMocks()
  localStorage.clear()
  document.documentElement.classList.remove('light', 'dark')
  document.body.removeAttribute('data-theme-preset')
})

describe('index.html theme boot script', () => {
  it('applies the saved dark preference to html before React mounts', () => {
    mockSystemDarkMode(false)
    localStorage.setItem(THEME_STORAGE_KEYS.mode, 'dark')

    runBootScript()

    expect(document.documentElement).toHaveClass('dark')
    expect(document.documentElement).not.toHaveClass('light')
  })

  it('applies the saved light preference even when the system is dark', () => {
    mockSystemDarkMode(true)
    localStorage.setItem(THEME_STORAGE_KEYS.mode, 'light')

    runBootScript()

    expect(document.documentElement).toHaveClass('light')
    expect(document.documentElement).not.toHaveClass('dark')
  })

  it('follows the system dark mode when no preference is saved', () => {
    mockSystemDarkMode(true)

    runBootScript()

    expect(document.documentElement).toHaveClass('dark')
  })

  it('follows the system light mode when the saved value is invalid', () => {
    mockSystemDarkMode(false)
    localStorage.setItem(THEME_STORAGE_KEYS.mode, 'sepia')

    runBootScript()

    expect(document.documentElement).toHaveClass('light')
  })
})

describe('index.html preset boot script', () => {
  it('applies the cached site default when the visitor has no preset', () => {
    cacheSiteDefaultPreset('new-utopia')

    runPresetBootScript()

    expect(document.body).toHaveAttribute('data-theme-preset', 'new-utopia')
  })

  it('prefers the preset the visitor picked over the site default', () => {
    cacheSiteDefaultPreset('new-utopia')
    localStorage.setItem(THEME_STORAGE_KEYS.preset, 'rose-garden')

    runPresetBootScript()

    expect(document.body).toHaveAttribute('data-theme-preset', 'rose-garden')
  })

  it('leaves the built-in default alone when the visitor picked it', () => {
    cacheSiteDefaultPreset('new-utopia')
    localStorage.setItem(THEME_STORAGE_KEYS.preset, 'default')

    runPresetBootScript()

    expect(document.body).not.toHaveAttribute('data-theme-preset')
  })

  it.each(['', 'not json', '{"state":{}}'])(
    'sets no preset when the cached site config is %j',
    (cached) => {
      localStorage.setItem('system-config-storage', cached)

      runPresetBootScript()

      expect(document.body).not.toHaveAttribute('data-theme-preset')
    }
  )
})
