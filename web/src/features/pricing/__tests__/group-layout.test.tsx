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
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import GroupLayoutEditor from '../components/group-layout-editor'
import {
  completePricingGroupLayout,
  movePricingGroup,
  PRICING_GROUP_LAYOUT_KEY,
  type PricingGroupLayoutItem,
} from '../lib/group-layout'

const saved: PricingGroupLayoutItem[] = [
  {
    id: 'drawer:a',
    title: 'Supplier A',
    groups: ['premium', 'free'],
    default_open: false,
  },
  { id: 'group:default', title: '', groups: ['default'], default_open: true },
]

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

function renderEditor() {
  const get = vi.spyOn(api, 'get').mockImplementation(async (url) => ({
    data: {
      success: true,
      data:
        url === '/api/group/'
          ? ['default', 'premium', 'free', 'new']
          : [{ key: PRICING_GROUP_LAYOUT_KEY, value: JSON.stringify(saved) }],
    },
  }))
  const put = vi
    .spyOn(api, 'put')
    .mockResolvedValue({ data: { success: true } })
  const onClose = vi.fn()
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  })
  render(
    <QueryClientProvider client={client}>
      <GroupLayoutEditor onClose={onClose} />
    </QueryClientProvider>
  )
  return { get, put, onClose }
}

it('retains saved positions, filters hidden groups, and appends new groups in stable order', () => {
  expect(
    completePricingGroupLayout(saved, [
      'new',
      'default',
      'premium',
      'another',
      'auto',
      'all',
    ]).map((item) => item.groups)
  ).toEqual([['premium'], ['default'], ['another'], ['new']])
  expect(
    completePricingGroupLayout([], ['z', 'a', 'z']).flatMap(
      (item) => item.groups
    )
  ).toEqual(['a', 'z'])
  const moved = movePricingGroup(saved, 'default', 'drawer:a')
  expect(moved[0].groups).toEqual(['premium', 'free', 'default'])
  expect(saved[0].groups).toEqual(['premium', 'free'])
})

it('saves drawer positions, member order, and group moves in one setting', async () => {
  const { put, onClose } = renderEditor()
  const user = userEvent.setup()
  await screen.findByRole('textbox', { name: 'Drawer name' })
  await user.click(screen.getByRole('button', { name: 'Move premium down' }))
  await user.selectOptions(
    screen.getByRole('combobox', { name: 'Drawer for default' }),
    'drawer:a'
  )
  await user.click(screen.getByRole('button', { name: 'Move Supplier A down' }))
  await user.click(screen.getByRole('checkbox', { name: 'Expand by default' }))
  await user.click(screen.getByRole('button', { name: 'Save' }))
  await waitFor(() => expect(onClose).toHaveBeenCalledTimes(1))
  expect(put).toHaveBeenCalledWith('/api/option/', {
    key: PRICING_GROUP_LAYOUT_KEY,
    value: JSON.stringify([
      { id: 'group:new', title: '', groups: ['new'], default_open: true },
      {
        id: 'drawer:a',
        title: 'Supplier A',
        groups: ['free', 'premium', 'default'],
        default_open: true,
      },
    ]),
  })
})

it('creates and renames a drawer, blocks blank names, and releases groups when a drawer is removed', async () => {
  const { put } = renderEditor()
  const user = userEvent.setup()
  await screen.findByRole('textbox', { name: 'Drawer name' })
  await user.click(screen.getByRole('button', { name: 'Add drawer' }))
  const input = screen.getAllByRole('textbox', { name: 'Drawer name' })[0]
  await user.clear(input)
  expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled()
  await user.type(input, 'Supplier B')
  const destination = screen.getByRole('combobox', {
    name: 'Drawer for premium',
  })
  const option = screen.getAllByRole<HTMLOptionElement>('option', {
    name: 'Supplier B',
  })[0]
  await user.selectOptions(destination, option.value)
  await user.click(screen.getByRole('button', { name: 'Remove Supplier A' }))
  expect(screen.getByRole('combobox', { name: 'Drawer for free' })).toHaveValue(
    ''
  )
  await user.click(screen.getByRole('button', { name: 'Save' }))
  await waitFor(() => expect(put).toHaveBeenCalledTimes(1))
  const payload = JSON.parse(
    (put.mock.calls[0][1] as { value: string }).value
  ) as PricingGroupLayoutItem[]
  expect(payload.find((item) => item.title === 'Supplier B')?.groups).toEqual([
    'premium',
  ])
  expect(payload.flatMap((item) => item.groups).sort()).toEqual([
    'default',
    'free',
    'new',
    'premium',
  ])
})

it('keeps unsaved changes after a failed save and allows retry', async () => {
  const { put, onClose } = renderEditor()
  put.mockResolvedValueOnce({
    data: { success: false, message: 'Failed to update setting' },
  })
  const user = userEvent.setup()
  const input = await screen.findByRole('textbox', { name: 'Drawer name' })
  await user.clear(input)
  await user.type(input, 'Renamed')
  await user.click(screen.getByRole('button', { name: 'Save' }))
  await waitFor(() =>
    expect(screen.getByRole('button', { name: 'Save' })).toBeEnabled()
  )
  expect(input).toHaveValue('Renamed')
  expect(onClose).not.toHaveBeenCalled()
  await user.click(screen.getByRole('button', { name: 'Save' }))
  await waitFor(() => expect(onClose).toHaveBeenCalledTimes(1))
})

it('adds visible, focused drawers without crypto.randomUUID and saves their distinct identities', async () => {
  vi.stubGlobal('crypto', {})
  const { put } = renderEditor()
  const user = userEvent.setup()
  await screen.findByRole('textbox', { name: 'Drawer name' })
  await user.click(screen.getByRole('button', { name: 'Add drawer' }))
  let inputs = screen.getAllByRole('textbox', { name: 'Drawer name' })
  expect(inputs).toHaveLength(2)
  expect(inputs[0]).toHaveValue('New drawer')
  expect(inputs[0]).toHaveFocus()
  await user.clear(inputs[0])
  await user.type(inputs[0], 'Supplier B')
  await user.click(screen.getByRole('button', { name: 'Add drawer' }))
  inputs = screen.getAllByRole('textbox', { name: 'Drawer name' })
  expect(inputs).toHaveLength(3)
  expect(inputs[0]).toHaveValue('New drawer')
  expect(inputs[0]).toHaveFocus()
  await user.click(screen.getByRole('button', { name: 'Save' }))
  await waitFor(() => expect(put).toHaveBeenCalledTimes(1))
  const payload = JSON.parse(
    (put.mock.calls[0][1] as { value: string }).value
  ) as PricingGroupLayoutItem[]
  expect(payload.slice(0, 2).map((item) => item.title)).toEqual([
    'New drawer',
    'Supplier B',
  ])
  expect(new Set(payload.map((item) => item.id)).size).toBe(payload.length)
})
