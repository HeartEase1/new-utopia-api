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
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { AlertTriangle, Loader2, Search, ShieldCheck } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Dialog } from '@/components/dialog'
import { PasswordInput } from '@/components/password-input'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { useDebounce } from '@/hooks/use-debounce'
import { formatQuota, parseQuotaFromDollars } from '@/lib/format'
import { handleServerError } from '@/lib/handle-server-error'

import {
  confirmWalletTransfer,
  createWalletTransferQuote,
  getWalletTransferStatus,
  searchWalletTransferRecipients,
} from '../../api'
import type {
  WalletTransferQuoteResult,
  WalletTransferRecipient,
} from '../../types'

interface WalletTransferDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  availableQuota: number
  onSetPaymentPassword: (intent: 'set' | 'change' | 'reset') => void
  onTransferred: () => void
}

export function WalletTransferDialog(props: WalletTransferDialogProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [keyword, setKeyword] = useState('')
  const debouncedKeyword = useDebounce(keyword, 300)
  const [recipient, setRecipient] = useState<WalletTransferRecipient | null>(
    null
  )
  const [amountInput, setAmountInput] = useState('')
  const [quote, setQuote] = useState<WalletTransferQuoteResult | null>(null)
  const [loginPassword, setLoginPassword] = useState('')
  const [paymentPassword, setPaymentPassword] = useState('')
  const [twoFACode, setTwoFACode] = useState('')
  const [confirmOpen, setConfirmOpen] = useState(false)
  const [preparing, setPreparing] = useState(false)
  const [transferring, setTransferring] = useState(false)

  const statusQuery = useQuery({
    queryKey: ['wallet-transfer-status'],
    queryFn: getWalletTransferStatus,
    enabled: props.open,
    staleTime: 0,
    meta: { errorToast: false },
  })
  const recipientQuery = useQuery({
    queryKey: ['wallet-transfer-recipients', debouncedKeyword],
    queryFn: () => searchWalletTransferRecipients(debouncedKeyword.trim()),
    enabled: props.open && debouncedKeyword.trim().length > 0,
    staleTime: 5000,
    meta: { errorToast: false },
  })
  const status = statusQuery.data
  const transferAmount = parseQuotaFromDollars(Number(amountInput))
  const remainingDailyQuota = status
    ? Math.max(0, status.daily_limit_quota - status.used_quota)
    : 0
  const maximumQuota = status?.two_fa_enabled
    ? props.availableQuota
    : Math.min(props.availableQuota, remainingDailyQuota)
  const exceedsAvailable = transferAmount > props.availableQuota
  const exceedsDaily =
    !status?.two_fa_enabled && transferAmount > remainingDailyQuota
  const canPrepare =
    recipient !== null &&
    status?.has_login_password === true &&
    status.has_payment_password &&
    status.attempt_count < status.attempt_limit &&
    Number.isFinite(transferAmount) &&
    transferAmount > 0 &&
    transferAmount <= maximumQuota
  const quoteExpired =
    quote !== null && quote.quote.expires_at * 1000 <= Date.now()
  const canConfirm =
    quote !== null &&
    !quoteExpired &&
    loginPassword.length > 0 &&
    paymentPassword.length > 0 &&
    (!status?.two_fa_enabled || twoFACode.trim().length > 0)

  const resetForm = () => {
    setKeyword('')
    setRecipient(null)
    setAmountInput('')
    setQuote(null)
    setLoginPassword('')
    setPaymentPassword('')
    setTwoFACode('')
    setConfirmOpen(false)
  }

  const handleOpenChange = (open: boolean) => {
    if (!open && !preparing && !transferring) resetForm()
    props.onOpenChange(open)
  }

  const handleSelectRecipient = (nextRecipient: WalletTransferRecipient) => {
    setRecipient(nextRecipient)
    setKeyword('')
    setQuote(null)
  }

  const handlePrepare = async () => {
    if (!recipient || !canPrepare) return
    setPreparing(true)
    setQuote(null)
    try {
      const nextQuote = await createWalletTransferQuote(
        recipient.id,
        transferAmount
      )
      setQuote(nextQuote)
    } catch (error) {
      handleServerError(error, t('Failed to prepare transfer'))
    } finally {
      setPreparing(false)
    }
  }

  const handleConfirm = async () => {
    if (!quote || !canConfirm) return
    setTransferring(true)
    try {
      await confirmWalletTransfer({
        quote_token: quote.quote_token,
        login_password: loginPassword,
        payment_password: paymentPassword,
        ...(status?.two_fa_enabled ? { two_fa_code: twoFACode.trim() } : {}),
      })
      setConfirmOpen(false)
      props.onOpenChange(false)
      resetForm()
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['wallet-transfer-status'] }),
        Promise.resolve(props.onTransferred()),
      ])
    } catch (error) {
      handleServerError(error, t('Transfer failed'))
      // The server spends a review on every submission, so a retry needs a new
      // one. The secrets are typed again; recipient and amount are kept.
      setConfirmOpen(false)
      setLoginPassword('')
      setPaymentPassword('')
      setTwoFACode('')
      setQuote(null)
      void queryClient.invalidateQueries({
        queryKey: ['wallet-transfer-status'],
      })
      try {
        setQuote(
          await createWalletTransferQuote(
            quote.quote.recipient_id,
            quote.quote.amount
          )
        )
      } catch {
        // Without a new review the form simply returns to the review step.
      }
    } finally {
      setTransferring(false)
    }
  }

  const showName = (user: WalletTransferRecipient | undefined) => {
    if (!user) return ''
    return user.display_name
      ? `${user.display_name} (@${user.username})`
      : `@${user.username}`
  }

  return (
    <>
      <Dialog
        open={props.open}
        onOpenChange={(open) => handleOpenChange(open)}
        title={t('Transfer balance')}
        description={t(
          'Find a recipient, review the fee, then confirm the transfer.'
        )}
        contentClassName='max-h-[calc(100dvh-1rem)] max-sm:w-[calc(100vw-1rem)] sm:max-w-xl'
        bodyClassName='space-y-4'
        footer={
          <div className='grid w-full grid-cols-2 gap-2'>
            <Button
              variant='outline'
              onClick={() => handleOpenChange(false)}
              disabled={preparing || transferring}
            >
              {t('Close')}
            </Button>
            <Button
              onClick={handlePrepare}
              disabled={!canPrepare || preparing || transferring}
            >
              {preparing && <Loader2 className='mr-2 size-4 animate-spin' />}
              {t('Review transfer')}
            </Button>
          </div>
        }
      >
        <div className='space-y-4 py-2'>
          {statusQuery.isLoading ? (
            <div className='text-muted-foreground flex items-center gap-2 text-sm'>
              <Loader2 className='size-4 animate-spin' />
              {t('Loading transfer security settings...')}
            </div>
          ) : null}

          {status?.two_fa_enabled ? (
            <Alert>
              <ShieldCheck />
              <AlertTitle>
                {t('Two-factor authentication is enabled')}
              </AlertTitle>
              <AlertDescription>
                {t(
                  'Your transfer amount is not capped today, with up to {{count}} attempts.',
                  { count: status.attempt_limit }
                )}
              </AlertDescription>
            </Alert>
          ) : (
            <Alert>
              <AlertTriangle />
              <AlertTitle>
                {t('Enable 2FA for higher transfer limits')}
              </AlertTitle>
              <AlertDescription>
                {t(
                  'Without 2FA, you can transfer up to {{amount}} per day and start up to {{count}} transfers. Enabling 2FA removes the daily amount cap and raises the attempt limit to 100.',
                  {
                    amount: status
                      ? formatQuota(status.daily_limit_quota)
                      : '100',
                    count: status?.attempt_limit ?? 10,
                  }
                )}
                <Button
                  size='sm'
                  variant='outline'
                  className='mt-2'
                  render={<Link to='/security' />}
                >
                  {t('Enable 2FA')}
                </Button>
              </AlertDescription>
            </Alert>
          )}

          {status && !status.has_login_password ? (
            <Alert variant='destructive'>
              <AlertTriangle />
              <AlertTitle>{t('A login password is required')}</AlertTitle>
              <AlertDescription>
                {t(
                  'Set a login password in your account security settings before transferring balance.'
                )}
              </AlertDescription>
            </Alert>
          ) : null}

          {status && !status.has_payment_password ? (
            <div className='flex flex-col gap-2 rounded-lg border p-3 sm:flex-row sm:items-center sm:justify-between'>
              <div>
                <p className='text-sm font-medium'>
                  {t('Payment password required')}
                </p>
                <p className='text-muted-foreground text-xs'>
                  {status.has_email
                    ? t(
                        'Set a separate payment password with email verification.'
                      )
                    : t(
                        'Bind an email address before setting a payment password.'
                      )}
                </p>
              </div>
              <Button
                size='sm'
                variant='outline'
                disabled={!status.has_email}
                onClick={() => {
                  props.onOpenChange(false)
                  props.onSetPaymentPassword('set')
                }}
              >
                {t('Set payment password')}
              </Button>
            </div>
          ) : null}

          {status && status.attempt_count >= status.attempt_limit ? (
            <Alert variant='destructive'>
              <AlertTriangle />
              <AlertDescription>
                {t('You have reached today’s transfer attempt limit.')}
              </AlertDescription>
            </Alert>
          ) : null}

          <div className='space-y-2'>
            <Label htmlFor='wallet-transfer-recipient-search'>
              {t('Recipient username or user ID')}
            </Label>
            <div className='relative'>
              <Search className='text-muted-foreground absolute top-1/2 left-3 size-4 -translate-y-1/2' />
              <Input
                id='wallet-transfer-recipient-search'
                value={keyword}
                onChange={(event) => {
                  setKeyword(event.target.value)
                  setRecipient(null)
                  setQuote(null)
                }}
                placeholder={t('Enter the full username or user ID')}
                className='pl-9'
                autoComplete='off'
              />
            </div>
            {recipientQuery.isFetching ? (
              <p className='text-muted-foreground text-xs'>
                {t('Searching...')}
              </p>
            ) : null}
            {recipientQuery.data && recipientQuery.data.length > 0 ? (
              <div className='max-h-40 space-y-1 overflow-y-auto rounded-lg border p-1'>
                {recipientQuery.data.map((candidate) => (
                  <Button
                    key={candidate.id}
                    type='button'
                    variant='ghost'
                    className='h-auto w-full justify-start py-2 text-left'
                    onClick={() => handleSelectRecipient(candidate)}
                  >
                    <span className='min-w-0 truncate'>
                      {showName(candidate)}
                      <span className='text-muted-foreground ml-2 text-xs'>
                        {t('User ID')}: {candidate.id}
                      </span>
                    </span>
                  </Button>
                ))}
              </div>
            ) : null}
            {debouncedKeyword.trim().length > 0 &&
            !recipientQuery.isFetching &&
            recipientQuery.data?.length === 0 ? (
              <p className='text-muted-foreground text-xs'>
                {t('No user has this exact username or ID.')}
              </p>
            ) : null}
            {recipient ? (
              <div className='bg-muted/40 flex items-center justify-between gap-3 rounded-lg border p-3'>
                <div className='min-w-0'>
                  <p className='truncate text-sm font-medium'>
                    {showName(recipient)}
                  </p>
                  <p className='text-muted-foreground text-xs'>
                    {t('User ID')}: {recipient.id}
                  </p>
                </div>
                <Button
                  type='button'
                  variant='ghost'
                  size='sm'
                  onClick={() => {
                    setRecipient(null)
                    setQuote(null)
                  }}
                >
                  {t('Change')}
                </Button>
              </div>
            ) : null}
          </div>

          <div className='space-y-2'>
            <Label htmlFor='wallet-transfer-amount'>
              {t('Transfer amount')}
            </Label>
            <Input
              id='wallet-transfer-amount'
              type='number'
              min='0'
              step='any'
              inputMode='decimal'
              value={amountInput}
              onChange={(event) => {
                setAmountInput(event.target.value)
                setQuote(null)
              }}
              placeholder={t('Enter amount')}
            />
            <div className='text-muted-foreground flex flex-wrap justify-between gap-x-3 text-xs'>
              <span>
                {t('Available balance')}: {formatQuota(props.availableQuota)}
              </span>
              {!status?.two_fa_enabled && status ? (
                <span>
                  {t('Daily remaining')}: {formatQuota(remainingDailyQuota)}
                </span>
              ) : null}
            </div>
            {exceedsAvailable ? (
              <p role='alert' className='text-destructive text-xs'>
                {t('The transfer amount exceeds your available balance.')}
              </p>
            ) : null}
            {exceedsDaily ? (
              <p role='alert' className='text-destructive text-xs'>
                {t('The transfer amount exceeds your remaining daily limit.')}
              </p>
            ) : null}
          </div>

          {quote && !quoteExpired ? (
            <div className='space-y-3 rounded-lg border p-3'>
              <div className='flex justify-between gap-3 text-sm'>
                <span className='text-muted-foreground'>
                  {t('Transfer amount')}
                </span>
                <span className='font-medium tabular-nums'>
                  {formatQuota(quote.quote.amount)}
                </span>
              </div>
              <div className='flex justify-between gap-3 text-sm'>
                <span className='text-muted-foreground'>
                  {t('Fee ({{percent}}%)', {
                    percent: quote.quote.fee_percent,
                  })}
                </span>
                <span className='font-medium tabular-nums'>
                  {formatQuota(quote.quote.fee)}
                </span>
              </div>
              <div className='flex justify-between gap-3 border-t pt-3 text-sm'>
                <span className='font-medium'>{t('Recipient receives')}</span>
                <span className='font-semibold tabular-nums'>
                  {formatQuota(quote.quote.received)}
                </span>
              </div>

              <div className='grid gap-3 sm:grid-cols-2'>
                <div className='space-y-1.5'>
                  <Label htmlFor='wallet-transfer-login-password'>
                    {t('Login password')}
                  </Label>
                  <PasswordInput
                    id='wallet-transfer-login-password'
                    value={loginPassword}
                    onChange={(event) => setLoginPassword(event.target.value)}
                    autoComplete='current-password'
                  />
                </div>
                <div className='space-y-1.5'>
                  <Label htmlFor='wallet-transfer-payment-password'>
                    {t('Payment password')}
                  </Label>
                  <PasswordInput
                    id='wallet-transfer-payment-password'
                    value={paymentPassword}
                    onChange={(event) => setPaymentPassword(event.target.value)}
                    autoComplete='off'
                  />
                </div>
              </div>
              {status?.two_fa_enabled ? (
                <div className='space-y-1.5'>
                  <Label htmlFor='wallet-transfer-2fa'>
                    {t('Two-factor authentication code')}
                  </Label>
                  <Input
                    id='wallet-transfer-2fa'
                    value={twoFACode}
                    onChange={(event) => setTwoFACode(event.target.value)}
                    autoComplete='one-time-code'
                    inputMode='numeric'
                    maxLength={32}
                  />
                  <p className='text-muted-foreground text-xs'>
                    {t(
                      'Enter a current authenticator code or an unused backup code.'
                    )}
                  </p>
                </div>
              ) : null}
              <Button
                type='button'
                className='w-full'
                disabled={!canConfirm || transferring}
                onClick={() => setConfirmOpen(true)}
              >
                {t('Confirm balance transfer')}
              </Button>
            </div>
          ) : null}
          {quoteExpired ? (
            <Alert variant='destructive'>
              <AlertTriangle />
              <AlertDescription>
                {t(
                  'This transfer quote has expired. Review the transfer again.'
                )}
              </AlertDescription>
            </Alert>
          ) : null}
        </div>
      </Dialog>

      {quote ? (
        <ConfirmDialog
          open={confirmOpen}
          onOpenChange={setConfirmOpen}
          title={t('Confirm balance transfer')}
          desc={t(
            'Review the recipient and amounts carefully. This transfer cannot be reversed.'
          )}
          confirmText={
            transferring ? t('Processing...') : t('Confirm transfer')
          }
          handleConfirm={handleConfirm}
          disabled={!canConfirm}
          isLoading={transferring}
        >
          <div className='space-y-2 rounded-lg border p-3 text-sm'>
            <p className='font-medium'>
              {t('To')}:{' '}
              {showName({
                id: quote.quote.recipient_id,
                username: quote.quote.recipient_username,
                display_name: quote.quote.recipient_display_name,
              })}
            </p>
            <p className='text-muted-foreground text-xs'>
              {t('User ID')}: {quote.quote.recipient_id}
            </p>
            <div className='flex justify-between gap-2'>
              <span>{t('Transfer amount')}</span>
              <span>{formatQuota(quote.quote.amount)}</span>
            </div>
            <div className='flex justify-between gap-2'>
              <span>{t('Fee')}</span>
              <span>{formatQuota(quote.quote.fee)}</span>
            </div>
            <div className='flex justify-between gap-2 border-t pt-2 font-semibold'>
              <span>{t('Recipient receives')}</span>
              <span>{formatQuota(quote.quote.received)}</span>
            </div>
          </div>
        </ConfirmDialog>
      ) : null}
    </>
  )
}
