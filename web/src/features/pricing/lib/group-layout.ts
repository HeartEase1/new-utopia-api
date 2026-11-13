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
export type PricingGroupLayoutItem = {
  id: string
  title: string
  groups: string[]
  default_open: boolean
}

export const PRICING_GROUP_LAYOUT_KEY = 'console_setting.pricing_group_layout'

export function parsePricingGroupLayout(raw: string): PricingGroupLayoutItem[] {
  if (!raw) return []
  try {
    const value: unknown = JSON.parse(raw)
    if (!Array.isArray(value)) return []
    return value.filter(
      (item): item is PricingGroupLayoutItem =>
        item != null &&
        typeof item === 'object' &&
        typeof item.id === 'string' &&
        typeof item.title === 'string' &&
        Array.isArray(item.groups) &&
        item.groups.every((g: unknown) => typeof g === 'string') &&
        typeof item.default_open === 'boolean'
    )
  } catch {
    return []
  }
}

// Keep saved positions; append new groups deterministically instead of relying
// on the iteration order of a Go map in the pricing response.
export function completePricingGroupLayout(
  layout: PricingGroupLayoutItem[],
  groups: string[],
  keepEmptyDrawers = false
): PricingGroupLayoutItem[] {
  const remaining = new Set(groups.filter((g) => g !== 'auto' && g !== 'all'))
  const result: PricingGroupLayoutItem[] = []
  const usedIds = new Set(layout.map((item) => item.id))
  for (const item of layout) {
    const members = item.groups.filter((group) => {
      if (!remaining.has(group)) return false
      remaining.delete(group)
      return true
    })
    if (members.length || (keepEmptyDrawers && item.title)) {
      result.push({ ...item, groups: members })
    }
  }
  for (const group of [...remaining].sort()) {
    let id = `group:${group}`
    while (usedIds.has(id)) id = `:${id}`
    usedIds.add(id)
    result.push({
      id,
      title: '',
      groups: [group],
      default_open: true,
    })
  }
  return result
}

export function movePricingGroup(
  layout: PricingGroupLayoutItem[],
  group: string,
  destination: string,
  drawerIds = new Set(
    layout.filter((item) => item.title !== '').map((item) => item.id)
  )
): PricingGroupLayoutItem[] {
  const next = layout
    .map((item) => ({
      ...item,
      groups: item.groups.filter((g) => g !== group),
    }))
    .filter((item) => drawerIds.has(item.id) || item.groups.length)
  const drawer = next.find(
    (item) => item.id === destination && drawerIds.has(item.id)
  )
  if (drawer) {
    drawer.groups.push(group)
  } else {
    next.push({
      id: `group:${group}`,
      title: '',
      groups: [group],
      default_open: true,
    })
  }
  return next
}
