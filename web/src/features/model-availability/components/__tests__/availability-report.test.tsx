/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  cleanup,
  fireEvent,
  render,
  screen,
  within,
} from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { getModelAvailability } from '@/features/performance-metrics/api'
import type {
  ModelAvailabilityData,
  ModelAvailabilityStats,
} from '@/features/performance-metrics/types'

import { AvailabilityReport } from '../availability-report'

vi.mock('@/features/performance-metrics/api', () => ({
  getModelAvailability: vi.fn(),
}))

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
  vi.unstubAllGlobals()
})

// The given [requests, successes] pairs fill the most recent hours.
function stats(
  requestCount: number,
  successRate: number,
  recentHours: [number, number][] = []
): ModelAvailabilityStats {
  const idle = Array.from({ length: 24 - recentHours.length }, () => 0)
  return {
    request_count: requestCount,
    success_rate: successRate,
    avg_latency_ms: 1500,
    avg_tps: 42,
    hourly_requests: [...idle, ...recentHours.map((hour) => hour[0])],
    hourly_successes: [...idle, ...recentHours.map((hour) => hour[1])],
  }
}

const availability: ModelAvailabilityData = {
  success: true,
  data: {
    window_start: 1_700_000_000,
    window_end: 1_700_086_399,
    summary: stats(400, 80),
    groups: [
      {
        group: 'premium',
        ...stats(300, 90),
        models: [
          { model_name: 'm-slow', ...stats(100, 70) },
          { model_name: 'm-fast', ...stats(200, 100) },
        ],
      },
      {
        group: 'default',
        ...stats(100, 50, [
          [0, 0],
          [5, 5],
          [100, 90],
          [100, 70],
          [100, 20],
          [100, 10],
        ]),
        models: [{ model_name: 'm-slow', ...stats(100, 50) }],
      },
    ],
    models: [
      {
        model_name: 'm-slow',
        ...stats(200, 60),
        groups: [
          { group: 'premium', ...stats(100, 70) },
          { group: 'default', ...stats(100, 50) },
        ],
      },
      {
        model_name: 'm-fast',
        ...stats(200, 100),
        groups: [{ group: 'premium', ...stats(200, 100) }],
      },
    ],
  },
}

function renderPage() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <AvailabilityReport />
    </QueryClientProvider>
  )
}

function rowOf(cell: HTMLElement): HTMLElement {
  const row = cell.closest('tr')
  if (!row) throw new Error('The cell is not inside a table row')
  return row
}

// Names of the body rows, top to bottom.
function rowNames(): string[] {
  const names = ['premium', 'default', 'm-slow', 'm-fast']
  return screen
    .getAllByRole('row')
    .slice(1)
    .map(
      (row) => names.find((name) => within(row).queryByText(name)) ?? 'unknown'
    )
}

describe('model availability', () => {
  it('lists groups in the received order with site totals and traffic share', async () => {
    vi.mocked(getModelAvailability).mockResolvedValue(availability)

    renderPage()

    const premium = rowOf(await screen.findByText('premium'))
    expect(rowNames()).toEqual(['premium', 'default'])
    expect(within(premium).getByText('90.00%')).toBeInTheDocument()
    expect(within(premium).getByText('75.0%')).toBeInTheDocument()
    expect(screen.getByText('80.00%')).toBeInTheDocument()
  })

  it('expands a group into its models ranked by availability, then collapses', async () => {
    vi.mocked(getModelAvailability).mockResolvedValue(availability)
    renderPage()
    const toggle = await screen.findByRole('button', {
      name: 'Expand details: premium',
    })
    expect(toggle).toHaveAttribute('aria-expanded', 'false')

    fireEvent.click(toggle)

    expect(rowNames()).toEqual(['premium', 'm-fast', 'm-slow', 'default'])
    const fast = rowOf(screen.getByText('m-fast'))
    expect(within(fast).getByText('66.7%')).toBeInTheDocument()
    const collapse = screen.getByRole('button', {
      name: 'Collapse details: premium',
    })
    expect(collapse).toHaveAttribute('aria-expanded', 'true')

    fireEvent.click(collapse)

    expect(rowNames()).toEqual(['premium', 'default'])
  })

  it('ranks models by availability and expands one into its groups', async () => {
    vi.mocked(getModelAvailability).mockResolvedValue(availability)
    renderPage()
    await screen.findByText('premium')

    fireEvent.click(screen.getByRole('tab', { name: 'By model' }))

    expect(rowNames()).toEqual(['m-fast', 'm-slow'])

    fireEvent.click(
      screen.getByRole('button', { name: 'Expand details: m-slow' })
    )

    expect(rowNames()).toEqual(['m-fast', 'm-slow', 'premium', 'default'])
  })

  it('draws 24 hourly cells graded by request volume and success rate', async () => {
    vi.mocked(getModelAvailability).mockResolvedValue(availability)
    renderPage()
    const row = rowOf(await screen.findByText('default'))

    const cells = [
      ...within(row).getByRole('img', {
        name: 'Hourly availability over the last 24 hours',
      }).children,
    ].map((cell) => cell.getAttribute('data-level'))

    expect(cells).toHaveLength(24)
    expect(cells.slice(0, 18)).toEqual(
      Array.from({ length: 18 }, () => 'empty')
    )
    expect(cells.slice(18)).toEqual([
      'empty',
      'insufficient',
      'healthy',
      'degraded',
      'poor',
      'down',
    ])
  })

  it('outlines only the hour that is still in progress', async () => {
    vi.mocked(getModelAvailability).mockResolvedValue(availability)
    renderPage()
    const row = rowOf(await screen.findByText('default'))

    const inProgress = [
      ...within(row).getByRole('img', {
        name: 'Hourly availability over the last 24 hours',
      }).children,
    ].map((cell) => cell.hasAttribute('data-in-progress'))

    expect(inProgress.lastIndexOf(false)).toBe(22)
    expect(inProgress.at(-1)).toBe(true)
  })

  it('searches by name and opens groups that only match through a model', async () => {
    vi.mocked(getModelAvailability).mockResolvedValue(availability)
    renderPage()
    await screen.findByText('premium')
    const search = screen.getByRole('searchbox', {
      name: 'Search groups or models',
    })

    fireEvent.change(search, { target: { value: 'FAST' } })

    expect(rowNames()).toEqual(['premium', 'm-fast'])

    fireEvent.change(search, { target: { value: 'defa' } })

    expect(rowNames()).toEqual(['default'])

    fireEvent.change(search, { target: { value: 'nothing-like-this' } })

    expect(screen.getByText('No matching results')).toBeInTheDocument()
    expect(screen.queryByRole('table')).not.toBeInTheDocument()
  })

  it('shows expandable cards instead of a table on a narrow screen', async () => {
    const desktopMatchMedia = window.matchMedia
    vi.stubGlobal('matchMedia', (query: string) => ({
      ...desktopMatchMedia(query),
      matches: query === '(max-width: 767px)',
    }))
    vi.mocked(getModelAvailability).mockResolvedValue(availability)
    renderPage()
    const list = await screen.findByRole('list', { name: 'Model availability' })
    expect(screen.queryByRole('table')).not.toBeInTheDocument()
    expect(within(list).getAllByRole('listitem')).toHaveLength(2)
    expect(within(list).getByText('90.00%')).toBeInTheDocument()

    fireEvent.click(
      within(list).getByRole('button', { name: 'Expand details: premium' })
    )

    expect(within(list).getAllByRole('listitem')).toHaveLength(4)
    expect(within(list).getByText('m-fast')).toBeInTheDocument()
    expect(
      within(list).getByRole('button', { name: 'Collapse details: premium' })
    ).toHaveAttribute('aria-expanded', 'true')
  })

  it('shows an empty state when nothing was requested in the window', async () => {
    vi.mocked(getModelAvailability).mockResolvedValue({
      success: true,
      data: {
        ...availability.data,
        summary: stats(0, 0),
        groups: [],
        models: [],
      },
    })

    renderPage()

    expect(
      await screen.findByText(
        'No model availability data for the last 24 hours.'
      )
    ).toBeInTheDocument()
    expect(screen.queryByRole('table')).not.toBeInTheDocument()
  })

  it('offers a retry after a failed load and recovers', async () => {
    vi.mocked(getModelAvailability)
      .mockRejectedValueOnce(new Error('offline'))
      .mockResolvedValue(availability)
    renderPage()
    expect(
      await screen.findByText('Could not load model availability.')
    ).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Retry' }))

    expect(await screen.findByText('premium')).toBeInTheDocument()
  })
})
