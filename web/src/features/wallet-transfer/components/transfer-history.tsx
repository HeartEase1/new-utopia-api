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
import { ArrowRight, ChevronLeft, ChevronRight, Search } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { CopyButton } from '@/components/copy-button'
import { Dialog } from '@/components/dialog'
import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { StatusBadge } from '@/components/status-badge'
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
        : getWalletTransferHistory(page, pageSize, debouncedKeyword.trim()),
    enabled: props.enabled ?? true,
    staleTime: 0,
    meta: { errorToast: false },
  })
  // The own history is a bare page, whose `page` field is only the page
  // number; the reconciliation wraps its page next to `total_fee`.
  let pageData: { items: WalletTransferRecord[]; total: number } | undefined
  if (historyQuery.data) {
    pageData =
      'total_fee' in historyQuery.data
        ? historyQuery.data.page
        : historyQuery.data
  }
  const records = pageData?.items ?? []
  const total = pageData?.total ?? 0
  const totalFees =
    historyQuery.data && 'total_fee' in historyQuery.data
      ? historyQuery.data.total_fee
      : 0
  const totalPages = Math.max(1, Math.ceil(total / pageSize))

  const resetPage = () => setPage(1)
  const renderRecord = (record: WalletTransferRecord) => {
    const sent = !isAdmin && record.sender_id === props.currentUserId
    const received = !isAdmin && record.recipient_id === props.currentUserId
    let direction: {
      label: string
      variant: 'neutral' | 'warning' | 'success'
    } = { label: t('Balance transfer'), variant: 'neutral' }
    if (sent) direction = { label: t('Transfer out'), variant: 'warning' }
    if (received) direction = { label: t('Transfer in'), variant: 'success' }

    return (
      <article
        key={record.id}
        aria-label={t('Order {{trade_no}}', { trade_no: record.trade_no })}
        className='rounded-lg border p-3 sm:p-4'
      >
        <div className='flex flex-wrap items-center justify-between gap-2'>
          <div className='flex items-center gap-2'>
            <StatusBadge
              variant={direction.variant}
              label={direction.label}
              copyable={false}
            />
            <span className='text-muted-foreground text-xs tabular-nums'>
              {formatTimestampToDate(
                Date.parse(record.created_at),
                'milliseconds'
              )}
            </span>
          </div>
          <span className='text-muted-foreground inline-flex items-center gap-1 text-xs'>
            {t('Order number')}:
            <span className='text-foreground font-mono'>{record.trade_no}</span>
            <CopyButton
              value={record.trade_no}
              size='sm'
              className='size-6 p-0'
              iconClassName='size-3.5'
              tooltip={t('Copy order number')}
              aria-label={t('Copy order number')}
            />
          </span>
        </div>

        <div className='mt-3 grid gap-2 sm:grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)] sm:items-center'>
          <TransferParty
            label={t('Sender')}
            username={record.sender_username}
            userId={record.sender_id}
            isYou={sent}
          />
          <ArrowRight
            aria-hidden='true'
            className='text-muted-foreground hidden size-4 sm:block'
          />
          <TransferParty
            label={t('Recipient')}
            username={record.recipient_username}
            userId={record.recipient_id}
            isYou={received}
          />
        </div>

        <dl className='bg-muted/40 mt-3 grid grid-cols-3 gap-3 rounded-md p-3'>
          <div className='min-w-0'>
            <dt className='text-muted-foreground text-xs'>
              {t('Transfer amount')}
            </dt>
            <dd className='mt-1 text-sm font-semibold tabular-nums'>
              {sent ? '−' : ''}
              {formatQuota(record.amount)}
            </dd>
          </div>
          <div className='min-w-0'>
            <dt className='text-muted-foreground text-xs'>
              {t('Transfer fee')} ({record.fee_percent}%)
            </dt>
            <dd className='mt-1 text-sm font-medium tabular-nums'>
              {formatQuota(record.fee)}
            </dd>
          </div>
          <div className='min-w-0'>
            <dt className='text-muted-foreground text-xs'>
              {t('Amount received')}
            </dt>
            <dd className='mt-1 text-sm font-semibold tabular-nums'>
              {received ? '+' : ''}
              {formatQuota(record.received)}
            </dd>
          </div>
        </dl>
      </article>
    )
  }

  return (
    <div className='space-y-4'>
      {isAdmin ? null : (
        <div className='space-y-1.5 sm:max-w-sm'>
          <Label htmlFor='wallet-transfer-history-order'>
            {t('Find by order number')}
          </Label>
          <div className='relative'>
            <Search className='text-muted-foreground absolute top-1/2 left-3 size-4 -translate-y-1/2' />
            <Input
              id='wallet-transfer-history-order'
              type='search'
              value={keyword}
              onChange={(event) => {
                setKeyword(event.target.value)
                resetPage()
              }}
              placeholder='WT2026…'
              className='pl-9 font-mono'
              autoComplete='off'
            />
          </div>
        </div>
      )}

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
                placeholder={t('User ID, username or order number')}
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

function TransferParty(props: {
  label: string
  username: string
  userId: number
  isYou: boolean
}) {
  const { t } = useTranslation()
  return (
    <div className='min-w-0'>
      <p className='text-muted-foreground text-xs'>{props.label}</p>
      <p className='truncate text-sm font-medium'>
        {props.username}
        {props.isYou ? (
          <span className='text-muted-foreground font-normal'>
            {' '}
            ({t('you')})
          </span>
        ) : null}
      </p>
      <p className='text-muted-foreground text-xs tabular-nums'>
        {t('User ID')}: {props.userId}
      </p>
    </div>
  )
}
