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
import { ChevronDown, ChevronRight } from 'lucide-react'
import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Progress } from '@/components/ui/progress'
import { TableCell, TableRow } from '@/components/ui/table'
import {
  formatLatency,
  formatThroughput,
  formatUptimePct,
} from '@/features/performance-metrics/lib/format'
import { formatNumber } from '@/lib/format'
import { cn } from '@/lib/utils'

import {
  AVAILABILITY_TEXT_CLASS,
  getAvailabilityLevel,
  type AvailabilityMode,
  type AvailabilityRow,
} from '../lib/availability'
import {
  AvailabilityHourStrip,
  type AvailabilityHourTooltipHandle,
} from './availability-hour-strip'

type AvailabilityRowProps = {
  row: AvailabilityRow
  windowStart: number
  windowEnd: number
  tooltip: AvailabilityHourTooltipHandle
  locale: string | undefined
  /** Set on a top-level row; a breakdown row has neither. */
  expanded?: boolean
  onToggle?: () => void
  /** Set on a breakdown row: the kind of row it belongs to. */
  parentKind?: AvailabilityMode
}

// Texts and grading shared by the table row and the card.
function useAvailabilityRowView(props: AvailabilityRowProps) {
  const { t } = useTranslation()
  const stats = props.row.stats

  let shareLabel = t('Share of site traffic')
  if (props.parentKind === 'group') {
    shareLabel = t("Share of this group's traffic")
  } else if (props.parentKind === 'model') {
    shareLabel = t("Share of this model's traffic")
  }

  let childCount = t('{{count}} models', { count: props.row.children.length })
  if (props.row.kind === 'model') {
    childCount = t('{{count}} groups', { count: props.row.children.length })
  }

  const toggleAction = props.expanded
    ? t('Collapse details')
    : t('Expand details')
  const level = getAvailabilityLevel(stats.request_count, stats.success_rate)

  return {
    isChild: props.parentKind !== undefined,
    shareLabel,
    childCount,
    toggleLabel: `${toggleAction}: ${props.row.name}`,
    sharePercent: props.row.share * 100,
    availabilityClassName: AVAILABILITY_TEXT_CLASS[level],
    availabilityTitle:
      level === 'insufficient' ? t('Insufficient samples') : undefined,
  }
}

function AvailabilityRowName(props: {
  row: AvailabilityRow
  isChild: boolean
  childCount: string
}) {
  return (
    <span className='block min-w-0 text-left'>
      <span
        title={props.row.name}
        className={cn(
          'block truncate',
          props.isChild ? 'text-xs' : 'font-medium',
          props.row.kind === 'model' && 'font-mono'
        )}
      >
        {props.row.name}
      </span>
      {!props.isChild && (
        <span className='text-muted-foreground block text-xs font-normal'>
          {props.childCount}
        </span>
      )}
    </span>
  )
}

export function AvailabilityTableRow(props: AvailabilityRowProps) {
  const view = useAvailabilityRowView(props)
  const stats = props.row.stats

  return (
    <TableRow
      className={cn(view.isChild ? 'bg-muted/30' : 'cursor-pointer')}
      onClick={props.onToggle}
    >
      <TableCell className='max-w-64'>
        <div
          className={cn(
            'flex min-w-0 items-center gap-2',
            view.isChild && 'pl-8'
          )}
        >
          {!view.isChild && (
            <Button
              variant='ghost'
              size='icon-xs'
              aria-label={view.toggleLabel}
              aria-expanded={props.expanded}
            >
              {props.expanded ? <ChevronDown /> : <ChevronRight />}
            </Button>
          )}
          <AvailabilityRowName
            row={props.row}
            isChild={view.isChild}
            childCount={view.childCount}
          />
        </div>
      </TableCell>
      <TableCell>
        <AvailabilityHourStrip
          stats={stats}
          windowStart={props.windowStart}
          windowEnd={props.windowEnd}
          tooltip={props.tooltip}
        />
      </TableCell>
      <TableCell
        title={view.availabilityTitle}
        className={cn(
          'text-right font-mono font-semibold tabular-nums',
          view.availabilityClassName
        )}
      >
        {formatUptimePct(stats.success_rate)}
      </TableCell>
      <TableCell className='text-right font-mono tabular-nums'>
        {formatLatency(stats.avg_latency_ms)}
      </TableCell>
      <TableCell className='text-right font-mono tabular-nums'>
        {formatThroughput(stats.avg_tps)}
      </TableCell>
      <TableCell className='text-right font-mono tabular-nums'>
        {formatNumber(stats.request_count, props.locale)}
      </TableCell>
      <TableCell title={view.shareLabel}>
        <div className='flex items-center gap-2'>
          <Progress
            value={view.sharePercent}
            aria-label={view.shareLabel}
            className='w-16 shrink-0'
          />
          <span className='text-muted-foreground font-mono text-xs tabular-nums'>
            {view.sharePercent.toFixed(1)}%
          </span>
        </div>
      </TableCell>
    </TableRow>
  )
}

/** The narrow-screen form of a row; `children` holds its breakdown cards. */
export function AvailabilityCard(
  props: AvailabilityRowProps & { children?: ReactNode }
) {
  const { t } = useTranslation()
  const view = useAvailabilityRowView(props)
  const stats = props.row.stats
  const name = (
    <AvailabilityRowName
      row={props.row}
      isChild={view.isChild}
      childCount={view.childCount}
    />
  )
  const metrics = [
    { label: t('Average latency'), value: formatLatency(stats.avg_latency_ms) },
    { label: t('Throughput'), value: formatThroughput(stats.avg_tps) },
    {
      label: t('Requests'),
      value: formatNumber(stats.request_count, props.locale),
    },
    { label: view.shareLabel, value: `${view.sharePercent.toFixed(1)}%` },
  ]

  return (
    <li className={cn(view.isChild ? 'bg-muted/40 rounded-lg p-3' : 'p-4')}>
      <div className='flex items-center justify-between gap-3'>
        {view.isChild ? (
          name
        ) : (
          <Button
            variant='ghost'
            aria-label={view.toggleLabel}
            aria-expanded={props.expanded}
            onClick={props.onToggle}
            className='h-auto min-w-0 flex-1 justify-start gap-2 px-0 py-0.5 hover:bg-transparent'
          >
            {props.expanded ? <ChevronDown /> : <ChevronRight />}
            {name}
          </Button>
        )}
        <span
          title={view.availabilityTitle}
          className={cn(
            'shrink-0 font-mono font-semibold tabular-nums',
            view.isChild ? 'text-sm' : 'text-base',
            view.availabilityClassName
          )}
        >
          {formatUptimePct(stats.success_rate)}
        </span>
      </div>
      <div className='mt-3'>
        <AvailabilityHourStrip
          stats={stats}
          windowStart={props.windowStart}
          windowEnd={props.windowEnd}
          tooltip={props.tooltip}
          fluid
        />
      </div>
      <dl className='mt-3 grid grid-cols-2 gap-x-4 gap-y-2 text-xs'>
        {metrics.map((metric) => (
          <div key={metric.label} className='min-w-0'>
            <dt className='text-muted-foreground truncate'>{metric.label}</dt>
            <dd className='mt-0.5 font-mono tabular-nums'>{metric.value}</dd>
          </div>
        ))}
      </dl>
      {props.children}
    </li>
  )
}
