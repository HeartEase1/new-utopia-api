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
import type {
  ModelAvailabilityData,
  ModelAvailabilityStats,
} from '@/features/performance-metrics/types'

/** Fewer requests than this is too small a sample to grade. */
export const AVAILABILITY_MIN_SAMPLES = 10

export type AvailabilityLevel =
  | 'healthy'
  | 'degraded'
  | 'poor'
  | 'down'
  | 'insufficient'
  | 'empty'

/**
 * Grades a success rate (0-100) over a request sample: 85% and above is
 * healthy, 65% to 85% degraded, 15% to 65% poor, below 15% down. Samples
 * under AVAILABILITY_MIN_SAMPLES requests are never graded.
 */
export function getAvailabilityLevel(
  requests: number,
  successRate: number
): AvailabilityLevel {
  if (!(requests > 0)) return 'empty'
  if (requests < AVAILABILITY_MIN_SAMPLES) return 'insufficient'
  if (successRate >= 85) return 'healthy'
  if (successRate >= 65) return 'degraded'
  if (successRate >= 15) return 'poor'
  return 'down'
}

export const AVAILABILITY_CELL_CLASS: Record<AvailabilityLevel, string> = {
  healthy: 'bg-emerald-500',
  degraded: 'bg-yellow-300',
  poor: 'bg-amber-800 dark:bg-amber-700',
  down: 'bg-red-500',
  insufficient: 'bg-muted-foreground/30',
  empty: 'border border-border bg-transparent',
}

export const AVAILABILITY_TEXT_CLASS: Record<AvailabilityLevel, string> = {
  healthy: 'text-emerald-600 dark:text-emerald-400',
  degraded: 'text-yellow-600 dark:text-yellow-300',
  poor: 'text-amber-800 dark:text-amber-500',
  down: 'text-red-600 dark:text-red-400',
  insufficient: 'text-muted-foreground',
  empty: 'text-muted-foreground',
}

export type AvailabilityMode = 'group' | 'model'

export type AvailabilityRow = {
  id: string
  kind: AvailabilityMode
  name: string
  stats: ModelAvailabilityStats
  /** Share of the parent's requests (of all requests for a top-level row), 0 to 1. */
  share: number
  children: AvailabilityRow[]
}

export function buildAvailabilityRows(
  data: ModelAvailabilityData['data'],
  mode: AvailabilityMode
): AvailabilityRow[] {
  const share = (count: number, total: number) =>
    total > 0 ? count / total : 0
  const total = data.summary?.request_count ?? 0
  if (mode === 'group') {
    return (data.groups ?? []).map((group) => ({
      id: `group:${group.group}`,
      kind: 'group',
      name: group.group,
      stats: group,
      share: share(group.request_count, total),
      children: (group.models ?? []).map((model) => ({
        id: `group:${group.group}:${model.model_name}`,
        kind: 'model',
        name: model.model_name,
        stats: model,
        share: share(model.request_count, group.request_count),
        children: [],
      })),
    }))
  }
  return (data.models ?? []).map((model) => ({
    id: `model:${model.model_name}`,
    kind: 'model',
    name: model.model_name,
    stats: model,
    share: share(model.request_count, total),
    children: (model.groups ?? []).map((group) => ({
      id: `model:${model.model_name}:${group.group}`,
      kind: 'group',
      name: group.group,
      stats: group,
      share: share(group.request_count, model.request_count),
      children: [],
    })),
  }))
}

/** Case-insensitive name match; an empty query matches everything. */
export function matchesAvailabilityQuery(name: string, query: string): boolean {
  return name.toLowerCase().includes(query.trim().toLowerCase())
}

/**
 * Keeps rows whose name matches the query. A row that only has matching
 * breakdown rows is kept too, narrowed to those matches.
 */
export function filterAvailabilityRows(
  rows: AvailabilityRow[],
  query: string
): AvailabilityRow[] {
  if (!query.trim()) return rows
  return rows.flatMap((row) => {
    if (matchesAvailabilityQuery(row.name, query)) return [row]
    const children = row.children.filter((child) =>
      matchesAvailabilityQuery(child.name, query)
    )
    return children.length > 0 ? [{ ...row, children }] : []
  })
}

export type AvailabilitySort =
  | 'default'
  | 'availability-desc'
  | 'availability-asc'
  | 'fastest'
  | 'slowest'
  | 'throughput-desc'
  | 'requests-desc'

/**
 * Sorts rows without mutating them. `default` keeps groups in the order
 * received (the model square's order) and ranks models by availability.
 * Rows that cannot be ranked (too few samples, no latency, no throughput)
 * always go last.
 */
export function sortAvailabilityRows(
  rows: AvailabilityRow[],
  sort: AvailabilitySort
): AvailabilityRow[] {
  if (sort === 'default') {
    return rows.every((row) => row.kind === 'group')
      ? rows
      : sortAvailabilityRows(rows, 'availability-desc')
  }
  const byRequests = (a: AvailabilityRow, b: AvailabilityRow) =>
    b.stats.request_count - a.stats.request_count
  if (sort === 'requests-desc') return [...rows].sort(byRequests)

  let value = (row: AvailabilityRow) => row.stats.success_rate
  let rankable = (row: AvailabilityRow) =>
    row.stats.request_count >= AVAILABILITY_MIN_SAMPLES
  if (sort === 'fastest' || sort === 'slowest') {
    value = (row) => row.stats.avg_latency_ms
    rankable = (row) => row.stats.avg_latency_ms > 0
  } else if (sort === 'throughput-desc') {
    value = (row) => row.stats.avg_tps
    rankable = (row) => row.stats.avg_tps > 0
  }
  const direction = sort === 'availability-asc' || sort === 'fastest' ? 1 : -1
  return [...rows].sort((a, b) => {
    if (rankable(a) !== rankable(b)) return rankable(a) ? -1 : 1
    if (!rankable(a)) return byRequests(a, b)
    return direction * (value(a) - value(b)) || byRequests(a, b)
  })
}
