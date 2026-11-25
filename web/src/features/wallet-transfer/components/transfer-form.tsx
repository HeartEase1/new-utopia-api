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
import axios from 'axios'
import { CheckCircle2, Loader2, Search, UserRound } from 'lucide-react'
import { useId, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { CopyButton } from '@/components/copy-button'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  confirmWalletTransfer,
  createWalletTransferQuote,
  searchWalletTransferRecipients,
} from '@/features/wallet/api'
import type {
  WalletTransferQuoteResult,
  WalletTransferRecipient,
  WalletTransferRecord,
  WalletTransferStatus,
} from '@/features/wallet/types'
import { useDebounce } from '@/hooks/use-debounce'
import {
  formatQuota,
  parseQuotaFromDollars,
  quotaUnitsToDollars,
} from '@/lib/format'
import { handleServerError } from '@/lib/handle-server-error'

import {
  SummaryRow,
  TransferConfirmDialog,
  type TransferCredentials,
} from './transfer-confirm-dialog'

/** True when the request may have reached the server without an answer. */
function isTransferOutcomeUnknown(error: unknown): boolean {
  if (!axios.isAxiosError(error)) return false
  const status = error.response?.status
  return status === undefined || status >= 500
}

interface TransferFormProps {
  /** Undefined while the sender's transfer settings are loading. */
  status: WalletTransferStatus | undefined
  availableQuota: number
  /** Called after a transfer went through, or was rejected and used an attempt. */
  onAttempted: () => void
}

/**
 * Recipient and amount. Pressing the transfer button opens one dialog that
 * shows what will be sent, then asks for the passwords.
 */
export function TransferForm(props: TransferFormProps) {
  const { t } = useTranslation()
  const ids = useId()
  const status = props.status
  const [keyword, setKeyword] = useState('')
  const debouncedKeyword = useDebounce(keyword, 300)
  const [recipient, setRecipient] = useState<WalletTransferRecipient | null>(
    null
  )
  const [amountInput, setAmountInput] = useState('')
  const [quote, setQuote] = useState<WalletTransferQuoteResult | null>(null)
  const [preparing, setPreparing] = useState(false)
  const [transferring, setTransferring] = useState(false)
  const [completed, setCompleted] = useState<WalletTransferRecord | null>(null)
  // Set while the last submission got no answer, to check it with the same review.
  const [unknownCredentials, setUnknownCredentials] =
    useState<TransferCredentials | null>(null)

  const searchTerm = debouncedKeyword.trim()
  const recipientQuery = useQuery({
    queryKey: ['wallet-transfer-recipients', searchTerm],
    queryFn: () => searchWalletTransferRecipients(searchTerm),
    enabled: searchTerm.length > 0 && recipient === null,
    staleTime: 5000,
    meta: { errorToast: false },
  })

  const transferAmount = parseQuotaFromDollars(Number(amountInput))
  // The root user and senders with 2FA have no daily amount cap.
  const dailyCapped =
    status !== undefined && !status.limits_exempt && !status.two_fa_enabled
  const remainingDailyQuota = status
    ? Math.max(0, status.daily_limit_quota - status.used_quota)
    : 0
  const maximumQuota = dailyCapped
    ? Math.min(props.availableQuota, remainingDailyQuota)
    : props.availableQuota
  const amountEntered = amountInput.trim() !== '' && transferAmount > 0
  const minimumQuota = status?.min_quota ?? 0
  const belowMinimum = amountEntered && transferAmount < minimumQuota
  const exceedsAvailable = transferAmount > props.availableQuota
  const exceedsDaily = dailyCapped && transferAmount > remainingDailyQuota
  const attemptsExhausted =
    status !== undefined &&
    !status.limits_exempt &&
    status.attempt_count >= status.attempt_limit
  const canPrepare =
    status !== undefined &&
    status.has_login_password &&
    status.has_payment_password &&
    !attemptsExhausted &&
    recipient !== null &&
    amountEntered &&
    !belowMinimum &&
    transferAmount <= maximumQuota

  const startOver = () => {
    setKeyword('')
    setRecipient(null)
    setAmountInput('')
    setCompleted(null)
    setQuote(null)
  }

  const handlePrepare = async () => {
    if (!recipient || !canPrepare) return
    setPreparing(true)
    try {
      setQuote(await createWalletTransferQuote(recipient.id, transferAmount))
    } catch (error) {
      handleServerError(error, t('Failed to prepare transfer'))
    } finally {
      setPreparing(false)
    }
  }

  const handleSubmit = async (credentials: TransferCredentials) => {
    if (!quote) return
    setTransferring(true)
    setUnknownCredentials(null)
    try {
      const record = await confirmWalletTransfer({
        quote_token: quote.quote_token,
        login_password: credentials.loginPassword,
        payment_password: credentials.paymentPassword,
        ...(status?.two_fa_enabled
          ? { two_fa_code: credentials.twoFACode.trim() }
          : {}),
      })
      setQuote(null)
      setCompleted(record)
    } catch (error) {
      // No answer means the transfer may have gone through. Keep this review:
      // sending it again returns the finished transfer, or runs it if it never
      // arrived, but can never move the balance twice.
      if (isTransferOutcomeUnknown(error)) {
        setUnknownCredentials(credentials)
        return
      }
      handleServerError(error, t('Transfer failed'))
      // The server spends a review on every submission, so the dialog closes
      // and a retry starts from the form, which keeps recipient and amount.
      setQuote(null)
    } finally {
      setTransferring(false)
      props.onAttempted()
    }
  }

  if (completed) {
    return (
      <div className='flex flex-col items-center gap-4 py-6 text-center'>
        <span className='bg-success/10 text-success flex size-14 items-center justify-center rounded-full'>
          <CheckCircle2 className='size-8' aria-hidden='true' />
        </span>
        <div>
          <h3 className='text-lg font-semibold'>{t('Transfer completed')}</h3>
          <p className='text-muted-foreground mt-1 text-sm'>
            {t('The balance has been credited to the recipient.')}
          </p>
        </div>
        <dl className='bg-muted/40 w-full max-w-sm space-y-2 rounded-lg border p-4 text-left'>
          <SummaryRow
            label={t('Order number')}
            value={
              <span className='inline-flex items-center gap-1 font-mono'>
                {completed.trade_no}
                <CopyButton
                  value={completed.trade_no}
                  size='sm'
                  className='size-6 p-0'
                  iconClassName='size-3.5'
                  tooltip={t('Copy order number')}
                  aria-label={t('Copy order number')}
                />
              </span>
            }
          />
          <SummaryRow
            label={t('Recipient')}
            value={`${completed.recipient_username} · ID ${completed.recipient_id}`}
          />
          <SummaryRow
            label={t('Transfer amount')}
            value={formatQuota(completed.amount)}
          />
          <SummaryRow
            label={t('Transfer fee')}
            value={formatQuota(completed.fee)}
          />
          <SummaryRow
            label={t('Recipient receives')}
            value={formatQuota(completed.received)}
            strong
          />
        </dl>
        <Button onClick={startOver}>{t('Make another transfer')}</Button>
      </div>
    )
  }

  const candidates = recipientQuery.data ?? []

  return (
    <div className='space-y-5'>
      <div className='space-y-2'>
        {recipient ? (
          <>
            <p className='text-sm leading-none font-medium'>{t('Recipient')}</p>
            <div className='bg-muted/40 flex items-center justify-between gap-3 rounded-lg border p-3'>
              <div className='flex min-w-0 items-center gap-3'>
                <span className='bg-primary/10 text-primary flex size-10 shrink-0 items-center justify-center rounded-full'>
                  <UserRound className='size-5' aria-hidden='true' />
                </span>
                <div className='min-w-0'>
                  <p className='truncate text-sm font-semibold'>
                    {recipient.username}
                  </p>
                  <p className='text-muted-foreground text-xs'>
                    {t('User ID')}: {recipient.id}
                  </p>
                </div>
              </div>
              <Button
                type='button'
                variant='outline'
                size='sm'
                onClick={() => setRecipient(null)}
              >
                {t('Change')}
              </Button>
            </div>
          </>
        ) : (
          <>
            <Label htmlFor={`${ids}-recipient`}>
              {t('Recipient username or user ID')}
            </Label>
            <div className='relative'>
              <Search
                aria-hidden='true'
                className='text-muted-foreground absolute top-1/2 left-3 size-4 -translate-y-1/2'
              />
              <Input
                id={`${ids}-recipient`}
                value={keyword}
                onChange={(event) => setKeyword(event.target.value)}
                placeholder={t('Enter the full username or user ID')}
                className='h-10 pl-9'
                autoComplete='off'
              />
            </div>
            <p className='text-muted-foreground text-xs'>
              {t(
                'Only an exact match is shown. Ask the recipient for their username or user ID.'
              )}{' '}
              {t('Each transfer goes to one recipient.')}
            </p>
            {recipientQuery.isFetching ? (
              <p className='text-muted-foreground flex items-center gap-2 text-xs'>
                <Loader2 className='size-3.5 animate-spin' aria-hidden='true' />
                {t('Searching...')}
              </p>
            ) : null}
            {candidates.map((candidate) => (
              <Button
                key={candidate.id}
                type='button'
                variant='outline'
                className='h-auto w-full justify-between gap-3 py-2.5 text-left'
                onClick={() => {
                  setRecipient(candidate)
                  setKeyword('')
                }}
              >
                <span className='min-w-0 truncate font-medium'>
                  {candidate.username}
                </span>
                <span className='text-muted-foreground shrink-0 text-xs'>
                  {t('User ID')}: {candidate.id}
                </span>
              </Button>
            ))}
            {searchTerm.length > 0 &&
            !recipientQuery.isFetching &&
            recipientQuery.isSuccess &&
            candidates.length === 0 ? (
              <p role='status' className='text-muted-foreground text-xs'>
                {t('No user has this exact username or ID.')}
              </p>
            ) : null}
          </>
        )}
      </div>

      <div className='space-y-2'>
        <Label htmlFor={`${ids}-amount`}>{t('Transfer amount')}</Label>
        <div className='flex gap-2'>
          <Input
            id={`${ids}-amount`}
            type='number'
            min='0'
            step='any'
            inputMode='decimal'
            value={amountInput}
            onChange={(event) => setAmountInput(event.target.value)}
            placeholder={t('Enter amount')}
            className='h-10 text-base tabular-nums'
          />
          <Button
            type='button'
            variant='outline'
            className='h-10 shrink-0'
            disabled={maximumQuota <= 0}
            onClick={() =>
              setAmountInput(String(quotaUnitsToDollars(maximumQuota)))
            }
          >
            {t('All')}
          </Button>
        </div>
        <div className='text-muted-foreground flex flex-wrap justify-between gap-x-4 gap-y-1 text-xs'>
          <span>
            {t('Available balance')}: {formatQuota(props.availableQuota)}
          </span>
          {dailyCapped ? (
            <span>
              {t('Daily remaining')}: {formatQuota(remainingDailyQuota)}
            </span>
          ) : null}
          {status ? (
            <span>
              {t('Minimum')}: {formatQuota(status.min_quota)}
            </span>
          ) : null}
          {status ? (
            <span>
              {t('Fee rate')}: {status.fee_percent}%
            </span>
          ) : null}
        </div>
        {belowMinimum ? (
          <p role='alert' className='text-destructive text-xs'>
            {t('The minimum transfer amount is {{amount}}.', {
              amount: formatQuota(minimumQuota),
            })}
          </p>
        ) : null}
        {amountEntered && exceedsAvailable ? (
          <p role='alert' className='text-destructive text-xs'>
            {t('The transfer amount exceeds your available balance.')}
          </p>
        ) : null}
        {amountEntered && !exceedsAvailable && exceedsDaily ? (
          <p role='alert' className='text-destructive text-xs'>
            {t('The transfer amount exceeds your remaining daily limit.')}
          </p>
        ) : null}
      </div>

      <Button
        type='button'
        size='lg'
        className='w-full'
        disabled={!canPrepare || preparing}
        onClick={handlePrepare}
      >
        {preparing ? <Loader2 className='mr-2 size-4 animate-spin' /> : null}
        {t('Transfer balance')}
      </Button>

      <TransferConfirmDialog
        quote={quote?.quote ?? null}
        availableQuota={props.availableQuota}
        twoFAEnabled={status?.two_fa_enabled === true}
        submitting={transferring}
        outcomeUnknown={unknownCredentials !== null}
        onCheckResult={() => {
          if (unknownCredentials) void handleSubmit(unknownCredentials)
        }}
        onCancel={() => {
          setQuote(null)
          setUnknownCredentials(null)
        }}
        onSubmit={handleSubmit}
      />
    </div>
  )
}
