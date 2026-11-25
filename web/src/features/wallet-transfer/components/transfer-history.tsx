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
import { useQuery } from '@tanstack/react-query'
import { ChevronLeft, ChevronRight, Search } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  getAdminWalletTransfers,
  getWalletTransferHistory,
} from '@/features/wallet/api'
import type { WalletTransferRecord } from '@/features/wallet/types'
import { useDebounce } from '@/hooks/use-debounce'
import { formatTimestampToDate, formatQuota } from '@/lib/format'

interface TransferHistoryProps {
  currentUserId: number
  /** Every user's transfers with search, date filter and fee total. */
  adminView?: boolean
  enabled?: boolean
}

const pageSize = 20

export function TransferHistory(props: TransferHistoryProps) {
  const { t } = useTranslation()
  const isAdmin = props.adminView === true
  const [page, setPage] = useState(1)
  const [keyword, setKeyword] = useState('')
  const [date, setDate] = useState('')
  const debouncedKeyword = useDebounce(keyword, 300)
  const historyQuery = useQuery({
    queryKey: [
      'wallet-transfer-history',
      isAdmin,
      page,
      pageSize,
      debouncedKeyword,
      date,
    ],
    queryFn: async () =>
      isAdmin
        ? getAdminWalletTransfers(page, pageSize, debouncedKeyword.trim(), date)
        : getWalletTransferHistory(page, pageSize),
    enabled: props.enabled ?? true,
    staleTime: 0,
    meta: { errorToast: false },
  })
  let pageData: { items: WalletTransferRecord[]; total: number } | undefined
  if (historyQuery.data) {
    if ('page' in historyQuery.data) {
      pageData = historyQuery.data.page
    } else {
      pageData = historyQuery.data
    }
  }
  const records = pageData?.items ?? []
  const total = pageData?.total ?? 0
  const totalFees =
    historyQuery.data && 'total_fee' in historyQuery.data
      ? historyQuery.data.total_fee
      : 0
  const totalPages = Math.max(1, Math.ceil(total / pageSize))

  const resetPage = () => setPage(1)
  const personName = (username: string, displayName: string, userId: number) =>
    displayName
      ? `${displayName} (@${username}) · #${userId}`
      : `@${username} · #${userId}`

  const renderRecord = (record: WalletTransferRecord) => {
    const sent = record.sender_id === props.currentUserId
    const otherUser = sent
      ? personName(
          record.recipient_username,
          record.recipient_display_name,
          record.recipient_id
        )
      : personName(
          record.sender_username,
          record.sender_display_name,
          record.sender_id
        )

    return (
      <article key={record.id} className='rounded-lg border p-3 sm:p-4'>
        <div className='flex flex-col gap-1 sm:flex-row sm:items-start sm:justify-between'>
          <div className='min-w-0'>
            {isAdmin ? (
              <>
                <p className='truncate text-sm font-medium'>
                  {t('From')}:{' '}
                  {personName(
                    record.sender_username,
                    record.sender_display_name,
                    record.sender_id
                  )}
                </p>
                <p className='mt-1 truncate text-sm font-medium'>
                  {t('To')}:{' '}
                  {personName(
                    record.recipient_username,
                    record.recipient_display_name,
                    record.recipient_id
                  )}
                </p>
              </>
            ) : (
              <p className='truncate text-sm font-medium'>
                {sent
                  ? t('Sent to {{user}}', { user: otherUser })
                  : t('Received from {{user}}', { user: otherUser })}
              </p>
            )}
            <p className='text-muted-foreground mt-1 text-xs'>
              {formatTimestampToDate(
                Date.parse(record.created_at),
                'milliseconds'
              )}
            </p>
          </div>
          <span className='text-muted-foreground shrink-0 text-xs'>
            {t('Fee rate')}: {record.fee_percent}%
          </span>
        </div>

        <div className='mt-3 grid grid-cols-2 gap-3 sm:grid-cols-3'>
          <div>
            <Label className='text-muted-foreground text-xs'>
              {t('Transfer amount')}
            </Label>
            <p className='mt-1 text-sm font-semibold tabular-nums'>
              {formatQuota(record.amount)}
            </p>
          </div>
          <div>
            <Label className='text-muted-foreground text-xs'>
              {t('Transfer fee')}
            </Label>
            <p className='mt-1 text-sm font-medium tabular-nums'>
              {formatQuota(record.fee)}
            </p>
          </div>
          <div>
            <Label className='text-muted-foreground text-xs'>
              {t(sent ? 'Recipient receives' : 'Amount received')}
            </Label>
            <p className='mt-1 text-sm font-semibold tabular-nums'>
              {formatQuota(record.received)}
            </p>
          </div>
        </div>
      </article>
    )
  }

  return (
    <div className='space-y-4'>
      {isAdmin ? (
        <div className='grid gap-3 sm:grid-cols-[minmax(0,1fr)_180px]'>
          <div className='space-y-1.5'>
            <Label htmlFor='wallet-transfer-history-search'>
              {t('Search users')}
            </Label>
            <div className='relative'>
              <Search className='text-muted-foreground absolute top-1/2 left-3 size-4 -translate-y-1/2' />
              <Input
                id='wallet-transfer-history-search'
                value={keyword}
                onChange={(event) => {
                  setKeyword(event.target.value)
                  resetPage()
                }}
                placeholder={t('Search by user ID or username')}
                className='pl-9'
              />
            </div>
          </div>
          <div className='space-y-1.5'>
            <Label htmlFor='wallet-transfer-history-date'>
              {t('Filter by date')}
            </Label>
            <Input
              id='wallet-transfer-history-date'
              type='date'
              value={date}
              onChange={(event) => {
                setDate(event.target.value)
                resetPage()
              }}
            />
          </div>
        </div>
      ) : null}

      {isAdmin ? (
        <div className='bg-muted/40 flex flex-wrap items-center justify-between gap-2 rounded-lg border px-3 py-2 text-sm'>
          <span className='text-muted-foreground'>
            {t('Total transfer fees in current results')}
          </span>
          <span className='font-semibold tabular-nums'>
            {formatQuota(totalFees)}
          </span>
        </div>
      ) : null}

      <div className='space-y-3'>
        {historyQuery.isLoading ? (
          <LoadingState
            className='min-h-40'
            message={t('Loading transfer history...')}
          />
        ) : null}
        {historyQuery.isError ? (
          <ErrorState
            className='min-h-40'
            title={t('Could not load transfer history')}
            onRetry={() => void historyQuery.refetch()}
          />
        ) : null}
        {!historyQuery.isLoading &&
        !historyQuery.isError &&
        records.length === 0 ? (
          <EmptyState
            className='min-h-40'
            title={t('No transfer records found')}
            description={t('Completed transfers will appear here.')}
          />
        ) : null}
        {records.map(renderRecord)}
      </div>

      {records.length > 0 ? (
        <div className='flex items-center justify-between border-t pt-3'>
          <p className='text-muted-foreground text-xs'>
            {t('Showing')} {(page - 1) * pageSize + 1}–
            {Math.min(page * pageSize, total)} {t('of')} {total}
          </p>
          <div className='flex items-center gap-2'>
            <Button
              variant='outline'
              size='icon-sm'
              aria-label={t('Previous page')}
              disabled={page <= 1 || historyQuery.isFetching}
              onClick={() => setPage((current) => Math.max(1, current - 1))}
            >
              <ChevronLeft className='size-4' />
            </Button>
            <span className='text-muted-foreground text-xs tabular-nums'>
              {page} / {totalPages}
            </span>
            <Button
              variant='outline'
              size='icon-sm'
              aria-label={t('Next page')}
              disabled={page >= totalPages || historyQuery.isFetching}
              onClick={() =>
                setPage((current) => Math.min(totalPages, current + 1))
              }
            >
              <ChevronRight className='size-4' />
            </Button>
          </div>
        </div>
      ) : null}
    </div>
  )
}

/** The reconciliation view for administrators, in a dialog. */
export function TransferReconciliationDialog(props: {
  open: boolean
  onOpenChange: (open: boolean) => void
  currentUserId: number
}) {
  const { t } = useTranslation()
  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={t('Wallet transfer reconciliation')}
      description={t(
        'Every user’s transfers on this site, with search, date filter and the total fees collected.'
      )}
      contentClassName='max-h-[calc(100dvh-1rem)] max-sm:w-[calc(100vw-1rem)] sm:max-w-4xl'
    >
      <div className='max-h-[min(70vh,640px)] overflow-y-auto py-2 pr-1'>
        <TransferHistory
          currentUserId={props.currentUserId}
          adminView
          enabled={props.open}
        />
      </div>
    </Dialog>
  )
}
