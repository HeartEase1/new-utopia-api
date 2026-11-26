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
import { AlertTriangle, Loader2 } from 'lucide-react'
import { useId, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { PasswordInput } from '@/components/password-input'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import type { WalletTransferQuote } from '@/features/wallet/types'
import { formatQuota } from '@/lib/format'
import { cn } from '@/lib/utils'

export function SummaryRow(props: {
  label: string
  value: ReactNode
  strong?: boolean
}) {
  return (
    <div className='flex items-baseline justify-between gap-3 text-sm'>
      <dt className='text-muted-foreground'>{props.label}</dt>
      <dd
        className={cn(
          'min-w-0 text-right tabular-nums',
          props.strong ? 'text-base font-semibold' : 'font-medium'
        )}
      >
        {props.value}
      </dd>
    </div>
  )
}

export type TransferCredentials = {
  loginPassword: string
  paymentPassword: string
  twoFACode: string
}

interface TransferConfirmDialogProps {
  /** The reviewed transfer; the dialog is open while it is set. */
  quote: WalletTransferQuote | null
  availableQuota: number
  twoFAEnabled: boolean
  submitting: boolean
  /** The last submission got no answer; it is checked with the same review. */
  outcomeUnknown: boolean
  onCheckResult: () => void
  onCancel: () => void
  onSubmit: (credentials: TransferCredentials) => void
}

/**
 * The one confirmation of a transfer: first the recipient and amounts to
 * check, then the passwords that authorize it.
 */
export function TransferConfirmDialog(props: TransferConfirmDialogProps) {
  const { t } = useTranslation()
  const ids = useId()
  const [verifying, setVerifying] = useState(false)
  const [loginPassword, setLoginPassword] = useState('')
  const [paymentPassword, setPaymentPassword] = useState('')
  const [twoFACode, setTwoFACode] = useState('')
  const quote = props.quote

  // Each reviewed transfer starts at the check step with empty passwords.
  const [shownQuote, setShownQuote] = useState(quote)
  if (shownQuote !== quote) {
    setShownQuote(quote)
    setVerifying(false)
    setLoginPassword('')
    setPaymentPassword('')
    setTwoFACode('')
  }

  const canSubmit =
    loginPassword.length > 0 &&
    paymentPassword.length > 0 &&
    (!props.twoFAEnabled || twoFACode.trim().length > 0)

  let footer = (
    <div className='grid w-full grid-cols-2 gap-2'>
      <Button variant='outline' onClick={props.onCancel}>
        {t('Back to edit')}
      </Button>
      <Button onClick={() => setVerifying(true)}>
        {t('Correct, continue')}
      </Button>
    </div>
  )
  if (props.outcomeUnknown) {
    footer = (
      <div className='grid w-full grid-cols-2 gap-2'>
        <Button
          variant='outline'
          disabled={props.submitting}
          onClick={props.onCancel}
        >
          {t('Check history later')}
        </Button>
        <Button disabled={props.submitting} onClick={props.onCheckResult}>
          {props.submitting ? (
            <Loader2 className='mr-2 size-4 animate-spin' />
          ) : null}
          {t('Check result')}
        </Button>
      </div>
    )
  } else if (verifying) {
    footer = (
      <div className='grid w-full grid-cols-2 gap-2'>
        <Button
          variant='outline'
          disabled={props.submitting}
          onClick={() => setVerifying(false)}
        >
          {t('Back')}
        </Button>
        <Button
          type='submit'
          form={`${ids}-form`}
          disabled={!canSubmit || props.submitting}
        >
          {props.submitting ? (
            <Loader2 className='mr-2 size-4 animate-spin' />
          ) : null}
          {t('Confirm transfer')}
        </Button>
      </div>
    )
  }

  let title = t('Check the transfer details')
  let description = t('Make sure the recipient and the amount are correct.')
  if (props.outcomeUnknown) {
    title = t('Transfer result not confirmed')
    description = t(
      'The connection dropped before the server answered, so this transfer may or may not have gone through.'
    )
  } else if (verifying) {
    title = t('Verify to transfer')
    description = t('Enter your passwords to authorize this transfer.')
  }

  return (
    <Dialog
      open={quote !== null}
      onOpenChange={(open) => {
        if (!open && !props.submitting) props.onCancel()
      }}
      title={title}
      description={description}
      contentClassName='max-sm:w-[calc(100vw-1rem)] sm:max-w-md'
      footer={footer}
    >
      {quote ? (
        <div className='space-y-4 py-2'>
          <dl className='bg-muted/40 space-y-2 rounded-lg border p-4'>
            <SummaryRow
              label={t('Recipient')}
              value={quote.recipient_username}
              strong
            />
            <SummaryRow label={t('User ID')} value={quote.recipient_id} />
            <SummaryRow
              label={t('Transfer amount')}
              value={formatQuota(quote.transfer_amount ?? quote.amount)}
            />
            <SummaryRow
              label={t('Fee ({{percent}}%)', { percent: quote.fee_percent })}
              value={formatQuota(quote.fee)}
            />
            <SummaryRow
              label={t('Fee payer')}
              value={
                quote.fee_payer === 'sender' ? t('Sender') : t('Recipient')
              }
            />
            <SummaryRow
              label={t('Total debited')}
              value={formatQuota(quote.amount)}
            />
            <div className='border-t pt-2'>
              <SummaryRow
                label={t('Recipient receives')}
                value={formatQuota(quote.received)}
                strong
              />
            </div>
            {verifying ? null : (
              <SummaryRow
                label={t('Your balance after the transfer')}
                value={formatQuota(
                  Math.max(0, props.availableQuota - quote.amount)
                )}
              />
            )}
          </dl>

          {props.outcomeUnknown ? (
            <Alert>
              <AlertTriangle />
              <AlertTitle>{t('Do not start a new transfer')}</AlertTitle>
              <AlertDescription>
                {t(
                  'Check result verifies this same transfer: if it went through you see the result, otherwise it is carried out now. The balance is never moved twice.'
                )}
              </AlertDescription>
            </Alert>
          ) : null}
          {!props.outcomeUnknown && verifying ? (
            <form
              id={`${ids}-form`}
              className='space-y-3'
              onSubmit={(event) => {
                event.preventDefault()
                if (!canSubmit || props.submitting) return
                props.onSubmit({ loginPassword, paymentPassword, twoFACode })
              }}
            >
              <div className='space-y-1.5'>
                <Label htmlFor={`${ids}-payment-password`}>
                  {t('Payment password')}
                </Label>
                <PasswordInput
                  id={`${ids}-payment-password`}
                  value={paymentPassword}
                  onChange={(event) => setPaymentPassword(event.target.value)}
                  autoComplete='off'
                  autoFocus
                />
              </div>
              <div className='space-y-1.5'>
                <Label htmlFor={`${ids}-login-password`}>
                  {t('Login password')}
                </Label>
                <PasswordInput
                  id={`${ids}-login-password`}
                  value={loginPassword}
                  onChange={(event) => setLoginPassword(event.target.value)}
                  autoComplete='current-password'
                />
              </div>
              {props.twoFAEnabled ? (
                <div className='space-y-1.5'>
                  <Label htmlFor={`${ids}-2fa`}>
                    {t('Two-factor authentication code')}
                  </Label>
                  <Input
                    id={`${ids}-2fa`}
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
              <p className='text-muted-foreground text-xs'>
                {t(
                  'The balance moves as soon as you confirm. Repeated wrong passwords lock transfers for a few minutes.'
                )}
              </p>
            </form>
          ) : null}
          {!props.outcomeUnknown && !verifying ? (
            <Alert variant='destructive'>
              <AlertTriangle />
              <AlertTitle>{t('A transfer cannot be undone')}</AlertTitle>
              <AlertDescription>
                {t(
                  'Once confirmed, the balance moves immediately and cannot be recalled or refunded. Check the username and user ID against what the recipient gave you.'
                )}
              </AlertDescription>
            </Alert>
          ) : null}
        </div>
      ) : null}
    </Dialog>
  )
}
