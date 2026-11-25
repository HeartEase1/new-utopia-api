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
import { Link } from '@tanstack/react-router'
import { KeyRound, ShieldAlert, ShieldCheck } from 'lucide-react'
import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Progress } from '@/components/ui/progress'
import { Skeleton } from '@/components/ui/skeleton'
import type { WalletTransferStatus } from '@/features/wallet/types'
import { formatQuota } from '@/lib/format'

function UsageMeter(props: {
  label: string
  used: ReactNode
  limit: ReactNode
  percent: number
}) {
  return (
    <div className='space-y-1.5'>
      <div className='flex items-baseline justify-between gap-3 text-xs'>
        <span className='text-muted-foreground'>{props.label}</span>
        <span className='font-medium tabular-nums'>
          {props.used} / {props.limit}
        </span>
      </div>
      <Progress value={Math.min(100, props.percent)} aria-label={props.label} />
    </div>
  )
}

interface TransferOverviewProps {
  /** Undefined while the sender's transfer settings are loading. */
  status: WalletTransferStatus | undefined
  availableQuota: number | undefined
  onManagePassword: () => void
}

/** Today's limits, the sender's security settings and the rules of a transfer. */
export function TransferOverview(props: TransferOverviewProps) {
  const { t } = useTranslation()
  const status = props.status

  return (
    <div className='space-y-4'>
      <Card>
        <CardHeader>
          <CardTitle className='text-sm'>{t("Today's allowance")}</CardTitle>
        </CardHeader>
        <CardContent className='space-y-4'>
          <div>
            <p className='text-muted-foreground text-xs'>
              {t('Available balance')}
            </p>
            {props.availableQuota === undefined ? (
              <Skeleton className='mt-1 h-7 w-28' />
            ) : (
              <p className='mt-0.5 text-2xl font-semibold tabular-nums'>
                {formatQuota(props.availableQuota)}
              </p>
            )}
          </div>
          {status ? (
            <>
              {status.limits_exempt || status.two_fa_enabled ? (
                <div className='flex items-baseline justify-between gap-3 text-xs'>
                  <span className='text-muted-foreground'>
                    {t('Transferred today')}
                  </span>
                  <span className='font-medium tabular-nums'>
                    {formatQuota(status.used_quota)} · {t('No daily cap')}
                  </span>
                </div>
              ) : (
                <UsageMeter
                  label={t('Transferred today')}
                  used={formatQuota(status.used_quota)}
                  limit={formatQuota(status.daily_limit_quota)}
                  percent={(status.used_quota / status.daily_limit_quota) * 100}
                />
              )}
              {status.limits_exempt ? (
                <div className='flex items-baseline justify-between gap-3 text-xs'>
                  <span className='text-muted-foreground'>
                    {t('Transfers started today')}
                  </span>
                  <span className='font-medium tabular-nums'>
                    {status.attempt_count} · {t('No attempt limit')}
                  </span>
                </div>
              ) : (
                <UsageMeter
                  label={t('Transfers started today')}
                  used={status.attempt_count}
                  limit={status.attempt_limit}
                  percent={(status.attempt_count / status.attempt_limit) * 100}
                />
              )}
              {status.limits_exempt ? null : (
                <p className='text-muted-foreground text-xs'>
                  {t(
                    'Limits reset at midnight (UTC+8). Rejected submissions also count as started transfers.'
                  )}
                </p>
              )}
            </>
          ) : (
            <Skeleton className='h-16 w-full' />
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className='text-sm'>{t('Transfer security')}</CardTitle>
        </CardHeader>
        <CardContent className='space-y-3'>
          <div className='flex items-center justify-between gap-3'>
            <div className='flex min-w-0 items-center gap-2 text-sm'>
              <KeyRound
                className='text-muted-foreground size-4'
                aria-hidden='true'
              />
              <span className='truncate'>{t('Payment password')}</span>
            </div>
            {status ? (
              <StatusBadge
                variant={status.has_payment_password ? 'success' : 'warning'}
                label={
                  status.has_payment_password
                    ? t('Configured')
                    : t('Not configured')
                }
                copyable={false}
              />
            ) : null}
          </div>
          <Button
            variant='outline'
            size='sm'
            className='w-full'
            disabled={!status}
            onClick={props.onManagePassword}
          >
            {status?.has_payment_password
              ? t('Change or reset payment password')
              : t('Set payment password')}
          </Button>

          <div className='flex items-center justify-between gap-3 border-t pt-3'>
            <div className='flex min-w-0 items-center gap-2 text-sm'>
              {status?.two_fa_enabled ? (
                <ShieldCheck className='text-success size-4' aria-hidden />
              ) : (
                <ShieldAlert className='text-warning size-4' aria-hidden />
              )}
              <span className='truncate'>{t('Two-Factor Authentication')}</span>
            </div>
            {status ? (
              <StatusBadge
                variant={status.two_fa_enabled ? 'success' : 'warning'}
                label={status.two_fa_enabled ? t('Enabled') : t('Disabled')}
                copyable={false}
              />
            ) : null}
          </div>
          {status && !status.two_fa_enabled ? (
            <Button
              variant='outline'
              size='sm'
              className='w-full'
              render={<Link to='/security' />}
            >
              {t('Enable 2FA')}
            </Button>
          ) : null}
        </CardContent>
      </Card>
    </div>
  )
}

/** The rules of a transfer, shown next to the form. */
export function TransferNotes() {
  const { t } = useTranslation()
  return (
    <Card>
      <CardHeader>
        <CardTitle className='text-sm'>{t('Before you transfer')}</CardTitle>
      </CardHeader>
      <CardContent>
        <ul className='text-muted-foreground list-disc space-y-2 pl-4 text-xs leading-relaxed'>
          <li>
            {t(
              'A completed transfer cannot be undone, recalled or refunded. The platform cannot reverse it for you.'
            )}
          </li>
          <li>
            {t(
              'Check both the username and the user ID. Usernames can look alike; the ID identifies exactly one account.'
            )}
          </li>
          <li>
            {t(
              'The fee is taken out of the amount you send, so the recipient receives the amount minus the fee.'
            )}
          </li>
          <li>
            {t(
              'Never share your payment password or verification codes. Staff will never ask for them.'
            )}
          </li>
          <li>
            {t(
              'Be careful with requests from strangers to transfer balance — a transfer sent by mistake or by fraud cannot be recovered.'
            )}
          </li>
        </ul>
      </CardContent>
    </Card>
  )
}
