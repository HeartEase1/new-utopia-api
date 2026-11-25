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
import { memo } from 'react'
import { useTranslation } from 'react-i18next'

import {
  TooltipTrigger,
  type createTooltipHandle,
} from '@/components/ui/tooltip'
import { formatUptimePct } from '@/features/performance-metrics/lib/format'
import type { ModelAvailabilityStats } from '@/features/performance-metrics/types'
import { toIntlLocale } from '@/i18n/languages'
import dayjs from '@/lib/dayjs'
import { formatNumber } from '@/lib/format'
import { cn } from '@/lib/utils'

import {
  AVAILABILITY_CELL_CLASS,
  getAvailabilityLevel,
} from '../lib/availability'

/** One hourly cell: `start` is the hour's first second (Unix time). */
export type AvailabilityHourCell = {
  start: number
  requests: number
  successRate: number
  /** The hour has not ended yet, so its numbers are still growing. */
  inProgress: boolean
}

export const AVAILABILITY_IN_PROGRESS_CLASS =
  'outline-muted-foreground/70 outline-1 outline-offset-1 outline-dashed'

export type AvailabilityHourTooltipHandle = ReturnType<
  typeof createTooltipHandle<AvailabilityHourCell>
>

/**
 * One cell per hour, oldest first. Hover details come from the single tooltip
 * that owns `tooltip`, so a long table does not mount a tooltip per cell.
 * The hour still running at `windowEnd` is outlined. `fluid` stretches the
 * cells across the available width instead of using their fixed size.
 */
export const AvailabilityHourStrip = memo(
  function AvailabilityHourStrip(props: {
    stats: ModelAvailabilityStats
    windowStart: number
    windowEnd: number
    tooltip: AvailabilityHourTooltipHandle
    fluid?: boolean
  }) {
    const { t } = useTranslation()
    return (
      <div
        role='img'
        aria-label={t('Hourly availability over the last 24 hours')}
        className={cn(
          'flex items-center gap-0.5',
          props.fluid ? 'w-full' : 'w-max'
        )}
      >
        {props.stats.hourly_requests.map((requests, index) => {
          const successes = props.stats.hourly_successes[index] ?? 0
          const successRate = requests > 0 ? (successes / requests) * 100 : 0
          const start = props.windowStart + index * 3600
          const level = getAvailabilityLevel(requests, successRate)
          const inProgress = start + 3600 > props.windowEnd
          return (
            <TooltipTrigger
              key={start}
              handle={props.tooltip}
              payload={{ start, requests, successRate, inProgress }}
              delay={0}
              render={
                <span
                  data-level={level}
                  data-in-progress={inProgress || undefined}
                  className={cn(
                    'h-6 rounded-[3px]',
                    props.fluid ? 'min-w-0 flex-1' : 'w-2 shrink-0 sm:w-2.5',
                    AVAILABILITY_CELL_CLASS[level],
                    inProgress && AVAILABILITY_IN_PROGRESS_CLASS
                  )}
                />
              }
            />
          )
        })}
      </div>
    )
  }
)

export function AvailabilityHourDetails(props: { cell: AvailabilityHourCell }) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const start = dayjs.unix(props.cell.start)
  const level = getAvailabilityLevel(
    props.cell.requests,
    props.cell.successRate
  )
  let status = `${t('Availability')}: ${formatUptimePct(props.cell.successRate)}`
  if (level === 'empty') {
    status = t('No requests')
  } else if (level === 'insufficient') {
    status = t('Insufficient samples')
  }
  return (
    <div className='flex flex-col gap-0.5 tabular-nums'>
      <span className='font-medium'>
        {start.format('MM-DD HH:mm')} – {start.add(1, 'hour').format('HH:mm')}
      </span>
      <span>
        {t('Requests')}: {formatNumber(props.cell.requests, locale)}
      </span>
      <span>{status}</span>
      {props.cell.inProgress && (
        <span>{t('Current hour, still in progress')}</span>
      )}
    </div>
  )
}
