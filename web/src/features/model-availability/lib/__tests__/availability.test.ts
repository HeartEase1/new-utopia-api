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
import { describe, expect, it } from 'vitest'

import type { ModelAvailabilityStats } from '@/features/performance-metrics/types'

import {
  buildAvailabilityRows,
  filterAvailabilityRows,
  getAvailabilityLevel,
  sortAvailabilityRows,
  type AvailabilityMode,
  type AvailabilityRow,
} from '../availability'

function stats(
  values: Partial<ModelAvailabilityStats> = {}
): ModelAvailabilityStats {
  return {
    request_count: 100,
    success_rate: 100,
    avg_latency_ms: 1000,
    avg_tps: 10,
    hourly_requests: [],
    hourly_successes: [],
    ...values,
  }
}

function row(
  name: string,
  kind: AvailabilityMode,
  values: Partial<ModelAvailabilityStats>
): AvailabilityRow {
  return { id: name, kind, name, stats: stats(values), share: 0, children: [] }
}

function names(rows: AvailabilityRow[]): string[] {
  return rows.map((item) => item.name)
}

describe('getAvailabilityLevel', () => {
  it.each([
    [0, 0, 'empty'],
    [9, 100, 'insufficient'],
    [10, 100, 'healthy'],
    [10, 85, 'healthy'],
    [10, 84.99, 'degraded'],
    [10, 65, 'degraded'],
    [10, 64.99, 'poor'],
    [10, 15, 'poor'],
    [10, 14.99, 'down'],
    [10, 0, 'down'],
  ])(
    'grades %i requests at %f%% success as %s',
    (requests, successRate, level) => {
      expect(getAvailabilityLevel(requests, successRate)).toBe(level)
    }
  )
})

describe('buildAvailabilityRows', () => {
  const data = {
    window_start: 0,
    window_end: 1,
    summary: stats({ request_count: 400 }),
    groups: [
      {
        group: 'premium',
        ...stats({ request_count: 300 }),
        models: [{ model_name: 'm1', ...stats({ request_count: 60 }) }],
      },
    ],
    models: [
      {
        model_name: 'm1',
        ...stats({ request_count: 100 }),
        groups: [{ group: 'premium', ...stats({ request_count: 60 }) }],
      },
    ],
  }

  it('measures a group against the site and its models against the group', () => {
    const [group] = buildAvailabilityRows(data, 'group')

    expect(group.share).toBe(0.75)
    expect(group.children[0].share).toBe(0.2)
  })

  it('measures a model against the site and its groups against the model', () => {
    const [model] = buildAvailabilityRows(data, 'model')

    expect(model.share).toBe(0.25)
    expect(model.children[0].share).toBe(0.6)
  })

  it('reports a zero share when the site has no requests', () => {
    const [group] = buildAvailabilityRows(
      { ...data, summary: stats({ request_count: 0 }) },
      'group'
    )

    expect(group.share).toBe(0)
  })
})

describe('sortAvailabilityRows', () => {
  const groups = [
    row('zeta', 'group', { success_rate: 50 }),
    row('alpha', 'group', { success_rate: 99 }),
  ]
  const models = [
    row('steady', 'model', { success_rate: 90, avg_latency_ms: 3000 }),
    row('rare', 'model', { success_rate: 100, request_count: 3 }),
    row('best', 'model', { success_rate: 99, avg_latency_ms: 500 }),
    row('untimed', 'model', { success_rate: 40, avg_latency_ms: 0 }),
  ]

  it('keeps groups in the received order by default', () => {
    expect(names(sortAvailabilityRows(groups, 'default'))).toEqual([
      'zeta',
      'alpha',
    ])
  })

  it('ranks models by highest availability by default, under-sampled last', () => {
    expect(names(sortAvailabilityRows(models, 'default'))).toEqual([
      'best',
      'steady',
      'untimed',
      'rare',
    ])
  })

  it('keeps under-sampled rows last when sorting by lowest availability', () => {
    expect(names(sortAvailabilityRows(models, 'availability-asc'))).toEqual([
      'untimed',
      'steady',
      'best',
      'rare',
    ])
  })

  it('sorts by latency with rows lacking latency last in both directions', () => {
    expect(names(sortAvailabilityRows(models, 'fastest'))).toEqual([
      'best',
      'rare',
      'steady',
      'untimed',
    ])
    expect(names(sortAvailabilityRows(models, 'slowest'))).toEqual([
      'steady',
      'rare',
      'best',
      'untimed',
    ])
  })

  it('sorts by highest throughput with rows lacking throughput last', () => {
    const rows = [
      row('idle', 'model', { avg_tps: 0 }),
      row('slow', 'model', { avg_tps: 12 }),
      row('quick', 'model', { avg_tps: 80 }),
    ]

    expect(names(sortAvailabilityRows(rows, 'throughput-desc'))).toEqual([
      'quick',
      'slow',
      'idle',
    ])
  })

  it('sorts by request count without mutating the input', () => {
    const original = names(models)

    expect(names(sortAvailabilityRows(models, 'requests-desc')).at(-1)).toBe(
      'rare'
    )
    expect(names(models)).toEqual(original)
  })
})

describe('filterAvailabilityRows', () => {
  const rows: AvailabilityRow[] = [
    {
      ...row('Premium', 'group', {}),
      children: [row('gpt-fast', 'model', {}), row('claude', 'model', {})],
    },
    { ...row('default', 'group', {}), children: [row('claude', 'model', {})] },
  ]

  it('returns every row for a blank query', () => {
    expect(filterAvailabilityRows(rows, '  ')).toBe(rows)
  })

  it('keeps a row matched by name, ignoring case, with all its breakdown rows', () => {
    const result = filterAvailabilityRows(rows, 'PREM')

    expect(names(result)).toEqual(['Premium'])
    expect(names(result[0].children)).toEqual(['gpt-fast', 'claude'])
  })

  it('keeps a row for its matching breakdown rows, narrowed to them', () => {
    const result = filterAvailabilityRows(rows, 'gpt')

    expect(names(result)).toEqual(['Premium'])
    expect(names(result[0].children)).toEqual(['gpt-fast'])
  })

  it('returns nothing when no name matches', () => {
    expect(filterAvailabilityRows(rows, 'missing')).toEqual([])
  })
})
