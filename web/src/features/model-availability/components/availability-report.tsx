/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import { useQuery } from '@tanstack/react-query'
import { Activity, Gauge, HeartPulse, Timer } from 'lucide-react'
import { Fragment, useMemo, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import {
  StaticDataTable,
  type StaticDataTableColumn,
} from '@/components/data-table'
import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { TableCell, TableRow } from '@/components/ui/table'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import {
  Tooltip,
  TooltipContent,
  createTooltipHandle,
} from '@/components/ui/tooltip'
import { MetricCell } from '@/features/dashboard/components/ui/metric-cell'
import { getModelAvailability } from '@/features/performance-metrics/api'
import {
  formatLatency,
  formatThroughput,
  formatUptimePct,
} from '@/features/performance-metrics/lib/format'
import { useMediaQuery } from '@/hooks'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber } from '@/lib/format'
import { requireServerSuccess } from '@/lib/server-error-message'
import { cn } from '@/lib/utils'

import {
  AVAILABILITY_CELL_CLASS,
  AVAILABILITY_MIN_SAMPLES,
  AVAILABILITY_TEXT_CLASS,
  buildAvailabilityRows,
  filterAvailabilityRows,
  matchesAvailabilityQuery,
  getAvailabilityLevel,
  sortAvailabilityRows,
  type AvailabilityLevel,
  type AvailabilityMode,
  type AvailabilityRow,
  type AvailabilitySort,
} from '../lib/availability'
import {
  AVAILABILITY_IN_PROGRESS_CLASS,
  AvailabilityHourDetails,
  type AvailabilityHourCell,
} from './availability-hour-strip'
import { AvailabilityCard, AvailabilityTableRow } from './availability-rows'

const REFRESH_INTERVAL_MS = 60 * 1000
const COLUMN_COUNT = 7

const DEFAULT_SORT: Record<AvailabilityMode, AvailabilitySort> = {
  group: 'default',
  model: 'availability-desc',
}

const LEGEND_LEVELS: AvailabilityLevel[] = [
  'healthy',
  'degraded',
  'poor',
  'down',
  'insufficient',
  'empty',
]

export function AvailabilityReport() {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const [mode, setMode] = useState<AvailabilityMode>('group')
  const [sortByMode, setSortByMode] = useState(DEFAULT_SORT)
  const [expandedRows, setExpandedRows] = useState<ReadonlySet<string>>(
    () => new Set()
  )
  const [search, setSearch] = useState('')
  const isNarrow = useMediaQuery('(max-width: 767px)')
  const [hourTooltip] = useState(() =>
    createTooltipHandle<AvailabilityHourCell>()
  )
  const availabilityQuery = useQuery({
    queryKey: ['model-availability', 24],
    queryFn: async () => requireServerSuccess(await getModelAvailability()),
    staleTime: REFRESH_INTERVAL_MS,
    refetchInterval: REFRESH_INTERVAL_MS,
    retry: false,
    meta: { errorToast: false },
  })
  const data = availabilityQuery.data?.data
  const sort = sortByMode[mode]
  const rows = useMemo(() => {
    if (!data) return []
    return sortAvailabilityRows(buildAvailabilityRows(data, mode), sort).map(
      (row) => ({ ...row, children: sortAvailabilityRows(row.children, sort) })
    )
  }, [data, mode, sort])
  const visibleRows = useMemo(
    () => filterAvailabilityRows(rows, search),
    [rows, search]
  )
  // A row kept only for its matching breakdown rows is shown opened.
  const isExpanded = (row: AvailabilityRow) =>
    expandedRows.has(row.id) ||
    (search.trim() !== '' && !matchesAvailabilityQuery(row.name, search))
  const toggleRow = (row: AvailabilityRow) =>
    setExpandedRows((current) => {
      const next = new Set(current)
      if (!next.delete(row.id)) next.add(row.id)
      return next
    })

  const sortOptions: { value: AvailabilitySort; label: string }[] = [
    { value: 'availability-desc', label: t('Highest availability') },
    { value: 'availability-asc', label: t('Lowest availability') },
    { value: 'fastest', label: t('Fastest') },
    { value: 'slowest', label: t('Slowest') },
    { value: 'throughput-desc', label: t('Highest throughput') },
    { value: 'requests-desc', label: t('Most requests') },
  ]
  if (mode === 'group') {
    sortOptions.unshift({ value: 'default', label: t('Model square order') })
  }

  const legendLabels: Record<AvailabilityLevel, string> = {
    healthy: '≥ 85%',
    degraded: '65% – 85%',
    poor: '15% – 65%',
    down: '< 15%',
    insufficient: t('Insufficient samples (under {{min}} requests)', {
      min: AVAILABILITY_MIN_SAMPLES,
    }),
    empty: t('No requests'),
  }

  const columns: StaticDataTableColumn<AvailabilityRow>[] = [
    { id: 'name', header: t(mode === 'group' ? 'Group' : 'Model') },
    { id: 'hours', header: t('Last 24 hours') },
    { id: 'availability', header: t('Availability'), className: 'text-right' },
    { id: 'latency', header: t('Average latency'), className: 'text-right' },
    { id: 'throughput', header: t('Throughput'), className: 'text-right' },
    { id: 'requests', header: t('Requests'), className: 'text-right' },
    { id: 'share', header: t('Traffic share'), className: 'w-36' },
  ]

  const summary = data?.summary
  const summaryLevel = getAvailabilityLevel(
    summary?.request_count ?? 0,
    summary?.success_rate ?? 0
  )

  let tableContent: ReactNode
  if (availabilityQuery.isLoading) {
    tableContent = (
      <div className='space-y-3 p-4'>
        {Array.from({ length: 5 }, (_, index) => (
          <Skeleton key={index} className='h-10 w-full' />
        ))}
      </div>
    )
  } else if (availabilityQuery.isError) {
    tableContent = (
      <ErrorState
        title={t('Could not load model availability.')}
        onRetry={() => void availabilityQuery.refetch()}
      />
    )
  } else if (!data || rows.length === 0) {
    tableContent = (
      <EmptyState
        description={t('No model availability data for the last 24 hours.')}
      />
    )
  } else if (visibleRows.length === 0) {
    tableContent = <EmptyState title={t('No matching results')} />
  } else if (isNarrow) {
    tableContent = (
      <ul aria-label={t('Model availability')} className='divide-y'>
        {visibleRows.map((row) => (
          <AvailabilityCard
            key={row.id}
            row={row}
            windowStart={data.window_start}
            windowEnd={data.window_end}
            tooltip={hourTooltip}
            locale={locale}
            expanded={isExpanded(row)}
            onToggle={() => toggleRow(row)}
          >
            {isExpanded(row) && row.children.length > 0 && (
              <ul className='mt-3 space-y-2'>
                {row.children.map((child) => (
                  <AvailabilityCard
                    key={child.id}
                    row={child}
                    windowStart={data.window_start}
                    windowEnd={data.window_end}
                    tooltip={hourTooltip}
                    locale={locale}
                    parentKind={row.kind}
                  />
                ))}
              </ul>
            )}
            {isExpanded(row) && row.children.length === 0 && (
              <p className='text-muted-foreground mt-3 text-center text-xs'>
                {t('No Data')}
              </p>
            )}
          </AvailabilityCard>
        ))}
      </ul>
    )
  } else {
    tableContent = (
      <StaticDataTable
        columns={columns}
        data={visibleRows}
        getRowKey={(row) => row.id}
        tableProps={{ 'aria-label': t('Model availability') }}
        renderRow={(row) => (
          <Fragment key={row.id}>
            <AvailabilityTableRow
              row={row}
              windowStart={data.window_start}
              windowEnd={data.window_end}
              tooltip={hourTooltip}
              locale={locale}
              expanded={isExpanded(row)}
              onToggle={() => toggleRow(row)}
            />
            {isExpanded(row) &&
              row.children.map((child) => (
                <AvailabilityTableRow
                  key={child.id}
                  row={child}
                  windowStart={data.window_start}
                  windowEnd={data.window_end}
                  tooltip={hourTooltip}
                  locale={locale}
                  parentKind={row.kind}
                />
              ))}
            {isExpanded(row) && row.children.length === 0 && (
              <TableRow className='bg-muted/30'>
                <TableCell
                  colSpan={COLUMN_COUNT}
                  className='text-muted-foreground py-3 text-center text-xs'
                >
                  {t('No Data')}
                </TableCell>
              </TableRow>
            )}
          </Fragment>
        )}
      />
    )
  }

  return (
    <section className='bg-card overflow-hidden rounded-xl border shadow-xs'>
      <div className='flex flex-wrap items-center justify-between gap-3 border-b px-4 py-3 sm:px-5'>
        <p className='text-muted-foreground text-xs'>
          {t('Based on request results from the last 24 hours.')}
        </p>
        <div className='flex flex-wrap items-center gap-2'>
          <Input
            type='search'
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            placeholder={t('Search groups or models')}
            aria-label={t('Search groups or models')}
            className='w-full sm:w-48'
          />
          <Tabs
            value={mode}
            onValueChange={(value) => {
              setMode(value === 'model' ? 'model' : 'group')
              setExpandedRows(new Set())
            }}
          >
            <TabsList>
              <TabsTrigger value='group'>{t('By group')}</TabsTrigger>
              <TabsTrigger value='model'>{t('By model')}</TabsTrigger>
            </TabsList>
          </Tabs>
          <Select
            items={sortOptions}
            value={sort}
            onValueChange={(value) =>
              setSortByMode((current) => ({
                ...current,
                [mode]: value as AvailabilitySort,
              }))
            }
          >
            <SelectTrigger aria-label={t('Sort')} className='min-w-40'>
              <SelectValue />
            </SelectTrigger>
            <SelectContent alignItemWithTrigger={false}>
              <SelectGroup>
                {sortOptions.map((option) => (
                  <SelectItem key={option.value} value={option.value}>
                    {option.label}
                  </SelectItem>
                ))}
              </SelectGroup>
            </SelectContent>
          </Select>
        </div>
      </div>

      <div className='grid grid-cols-2 gap-2 border-b p-4 sm:px-5 lg:grid-cols-4'>
        <MetricCell
          icon={HeartPulse}
          label={t('Site availability')}
          value={formatUptimePct(
            summary && summary.request_count > 0
              ? summary.success_rate
              : Number.NaN
          )}
          loading={availabilityQuery.isLoading}
          valueClassName={AVAILABILITY_TEXT_CLASS[summaryLevel]}
          tone='success'
        />
        <MetricCell
          icon={Timer}
          label={t('Average latency')}
          value={formatLatency(summary?.avg_latency_ms ?? 0)}
          loading={availabilityQuery.isLoading}
          tone='warning'
        />
        <MetricCell
          icon={Gauge}
          label={t('Throughput')}
          value={formatThroughput(summary?.avg_tps ?? 0)}
          loading={availabilityQuery.isLoading}
          tone='info'
        />
        <MetricCell
          icon={Activity}
          label={t('Total requests')}
          value={summary ? formatNumber(summary.request_count, locale) : '—'}
          loading={availabilityQuery.isLoading}
          tone='primary'
        />
      </div>

      <ul
        aria-label={t('Availability legend')}
        className='text-muted-foreground flex flex-wrap items-center gap-x-4 gap-y-1.5 border-b px-4 py-2.5 text-xs sm:px-5'
      >
        {LEGEND_LEVELS.map((level) => (
          <li key={level} className='flex items-center gap-1.5'>
            <span
              aria-hidden='true'
              className={cn(
                'size-3 rounded-[3px]',
                AVAILABILITY_CELL_CLASS[level]
              )}
            />
            {legendLabels[level]}
          </li>
        ))}
        <li className='flex items-center gap-1.5'>
          <span
            aria-hidden='true'
            className={cn(
              'bg-muted-foreground/30 mx-0.5 size-3 rounded-[3px]',
              AVAILABILITY_IN_PROGRESS_CLASS
            )}
          />
          {t('Current hour (in progress)')}
        </li>
      </ul>

      {tableContent}

      <Tooltip handle={hourTooltip}>
        {({ payload }) => (
          <TooltipContent>
            {payload !== undefined && (
              <AvailabilityHourDetails cell={payload} />
            )}
          </TooltipContent>
        )}
      </Tooltip>
    </section>
  )
}
