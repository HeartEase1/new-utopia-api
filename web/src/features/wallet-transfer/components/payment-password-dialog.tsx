/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { zodResolver } from '@hookform/resolvers/zod'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Loader2, MailCheck } from 'lucide-react'
import { useEffect, useState, type ReactNode } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'

import { Dialog } from '@/components/dialog'
import { ErrorState } from '@/components/error-state'
import { PasswordInput } from '@/components/password-input'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  completeWalletTransferPasswordFlow,
  getWalletTransferStatus,
  startWalletTransferPasswordFlow,
} from '@/features/wallet/api'
import type { WalletTransferPasswordFlow } from '@/features/wallet/types'
import { useCountdown } from '@/hooks/use-countdown'
import { handleServerError } from '@/lib/handle-server-error'
import { accountPasswordSchema } from '@/lib/password-policy'

type PasswordIntent = 'set' | 'change' | 'reset'

const formSchema = z
  .object({
    code: z.string(),
    currentPassword: z.string(),
    loginPassword: z.string(),
    twoFACode: z.string(),
    newPassword: accountPasswordSchema,
    confirmPassword: z.string().min(1, 'Please confirm your password.'),
  })
  .refine((values) => values.newPassword === values.confirmPassword, {
    message: "Passwords don't match.",
    path: ['confirmPassword'],
  })

type PasswordFormValues = z.infer<typeof formSchema>

interface PaymentPasswordDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  intent?: 'set' | 'change'
}

export function PaymentPasswordDialog(props: PaymentPasswordDialogProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const statusQuery = useQuery({
    queryKey: ['wallet-transfer-status'],
    queryFn: getWalletTransferStatus,
    enabled: props.open,
    staleTime: 0,
    meta: { errorToast: false },
  })
  const status = statusQuery.data
  const hasPaymentPassword = status?.has_payment_password
  const {
    secondsLeft,
    isActive,
    start: startCountdown,
    reset: resetCountdown,
  } = useCountdown({ initialSeconds: 60 })
  const [intent, setIntent] = useState<PasswordIntent>(props.intent ?? 'set')
  const [flow, setFlow] = useState<WalletTransferPasswordFlow | null>(null)
  const [sending, setSending] = useState(false)
  const [saving, setSaving] = useState(false)
  const form = useForm<PasswordFormValues>({
    resolver: zodResolver(formSchema),
    mode: 'onChange',
    defaultValues: {
      code: '',
      currentPassword: '',
      loginPassword: '',
      twoFACode: '',
      newPassword: '',
      confirmPassword: '',
    },
  })

  useEffect(() => {
    if (!props.open || hasPaymentPassword === undefined) return
    setIntent(props.intent ?? (hasPaymentPassword ? 'change' : 'set'))
  }, [props.open, props.intent, hasPaymentPassword])

  const resetDialog = () => {
    setIntent(props.intent ?? (status?.has_payment_password ? 'change' : 'set'))
    setFlow(null)
    setSending(false)
    setSaving(false)
    resetCountdown()
    form.reset()
  }

  const handleOpenChange = (open: boolean) => {
    if (!open && (sending || saving)) return
    if (!open) resetDialog()
    props.onOpenChange(open)
  }

  const handleIntentChange = (nextIntent: PasswordIntent) => {
    setIntent(nextIntent)
    setFlow(null)
    form.reset()
    resetCountdown()
  }

  // Setting or resetting is confirmed with the login password.
  // The root user without a bound email skips the email step.
  const emailReady = status?.has_email === true || status?.email_exempt === true
  const canStart =
    emailReady && (intent === 'change' || status?.has_login_password === true)

  const handleSendCode = async () => {
    if (!canStart || sending || isActive) return
    setSending(true)
    try {
      const result = await startWalletTransferPasswordFlow(intent)
      form.reset()
      setFlow(result)
      if (!result.email_exempt) {
        startCountdown(60)
        toast.success(t('Verification email sent'))
      }
    } catch (error) {
      handleServerError(error, t('Failed to send verification email'))
    } finally {
      setSending(false)
    }
  }

  const handleSubmit = form.handleSubmit(async (values) => {
    if (!flow) return
    if (Date.now() >= flow.expires_at * 1000) {
      setFlow(null)
      form.reset()
      toast.error(t('This verification flow has expired. Start again.'))
      return
    }
    if (!flow.email_exempt && !/^\d{6}$/.test(values.code)) {
      form.setError('code', {
        type: 'manual',
        message: 'Please enter the 6-digit code.',
      })
      return
    }
    if (intent === 'change' && !values.currentPassword) {
      form.setError('currentPassword', {
        type: 'manual',
        message: 'Please enter your current payment password.',
      })
      return
    }
    if (intent !== 'change' && !values.loginPassword) {
      form.setError('loginPassword', {
        type: 'manual',
        message: 'Please enter your login password.',
      })
      return
    }
    if (
      intent === 'reset' &&
      status?.two_fa_enabled &&
      !values.twoFACode.trim()
    ) {
      form.setError('twoFACode', {
        type: 'manual',
        message: 'Please enter your two-factor authentication code.',
      })
      return
    }

    setSaving(true)
    try {
      await completeWalletTransferPasswordFlow({
        flow_token: flow.flow_token,
        code: values.code,
        new_password: values.newPassword,
        ...(intent === 'change'
          ? { current_password: values.currentPassword }
          : {}),
        ...(intent === 'change'
          ? {}
          : { login_password: values.loginPassword }),
        ...(intent === 'reset' && status?.two_fa_enabled
          ? { two_fa_code: values.twoFACode.trim() }
          : {}),
      })
      toast.success(
        t(
          intent === 'set' ? 'Payment password set' : 'Payment password updated'
        )
      )
      await queryClient.invalidateQueries({
        queryKey: ['wallet-transfer-status'],
      })
      resetDialog()
      props.onOpenChange(false)
    } catch (error) {
      handleServerError(error, t('Failed to save payment password'))
    } finally {
      setSaving(false)
    }
  })

  const errorText = (name: keyof PasswordFormValues) => {
    const message = form.formState.errors[name]?.message
    return message ? (
      <p role='alert' className='text-destructive text-xs'>
        {t(message)}
      </p>
    ) : null
  }

  let startPrompt = t(
    'Bind an email address before managing the payment password.'
  )
  if (status?.email_exempt) {
    startPrompt = t(
      'As the super administrator you manage the payment password without email verification.'
    )
  } else if (status?.has_email) {
    startPrompt = t('We will send a one-time code to {{email}}.', {
      email: status.email,
    })
  }

  let resendAction: ReactNode
  if (sending) {
    resendAction = <Loader2 className='size-4 animate-spin' />
  } else if (isActive) {
    resendAction = t('Resend ({{seconds}}s)', { seconds: secondsLeft })
  } else {
    resendAction = t('Resend code')
  }

  return (
    <Dialog
      open={props.open}
      onOpenChange={handleOpenChange}
      title={t('Payment password')}
      description={t(
        'Manage the separate password used to authorize wallet transfers.'
      )}
      contentClassName='max-h-[calc(100dvh-1rem)] max-sm:w-[calc(100vw-1rem)] sm:max-w-lg'
      bodyClassName='space-y-4'
      footer={
        <div className='grid w-full grid-cols-2 gap-2'>
          <Button
            variant='outline'
            type='button'
            onClick={() => handleOpenChange(false)}
            disabled={sending || saving}
          >
            {t('Close')}
          </Button>
          {flow ? (
            <Button
              type='submit'
              form='wallet-transfer-password-form'
              disabled={!form.formState.isValid || saving}
            >
              {saving && <Loader2 className='mr-2 size-4 animate-spin' />}
              {t('Save payment password')}
            </Button>
          ) : (
            <Button
              type='button'
              onClick={handleSendCode}
              disabled={!canStart || sending || isActive}
            >
              {sending ? (
                <Loader2 className='mr-2 size-4 animate-spin' />
              ) : (
                <MailCheck className='mr-2 size-4' />
              )}
              {status?.email_exempt
                ? t('Continue')
                : t('Send verification code')}
            </Button>
          )}
        </div>
      }
    >
      <div className='space-y-4 py-2'>
        {statusQuery.isLoading ? (
          <p className='text-muted-foreground text-sm'>
            {t('Loading transfer security settings...')}
          </p>
        ) : null}
        {statusQuery.isError ? (
          <ErrorState
            className='min-h-24'
            title={t('Could not load transfer security settings.')}
            onRetry={() => void statusQuery.refetch()}
          />
        ) : null}
        {status && !emailReady ? (
          <Alert variant='destructive'>
            <AlertDescription>
              {t('Bind an email address before managing the payment password.')}
            </AlertDescription>
          </Alert>
        ) : null}

        {status?.has_payment_password ? (
          <div className='flex flex-wrap gap-2'>
            <Button
              type='button'
              size='sm'
              variant={intent === 'change' ? 'default' : 'outline'}
              onClick={() => handleIntentChange('change')}
            >
              {t('Change payment password')}
            </Button>
            <Button
              type='button'
              size='sm'
              variant={intent === 'reset' ? 'default' : 'outline'}
              onClick={() => handleIntentChange('reset')}
            >
              {t('Forgot payment password?')}
            </Button>
          </div>
        ) : null}

        {emailReady &&
        status?.has_login_password === false &&
        intent !== 'change' ? (
          <Alert variant='destructive'>
            <AlertDescription>
              {t(
                'Set a login password in your account security settings before transferring balance.'
              )}
            </AlertDescription>
          </Alert>
        ) : null}

        {intent === 'set' ? (
          <Alert>
            <AlertDescription>
              {t(
                'Setting a payment password requires your login password and email verification.'
              )}
            </AlertDescription>
          </Alert>
        ) : null}

        {intent === 'reset' ? (
          <Alert>
            <AlertDescription>
              {t(
                'Resetting requires your login password, email verification, and 2FA when enabled.'
              )}
            </AlertDescription>
          </Alert>
        ) : null}

        {flow ? (
          <form
            id='wallet-transfer-password-form'
            className='space-y-4'
            onSubmit={handleSubmit}
            noValidate
          >
            {flow.email_exempt ? null : (
              <>
                <p className='text-muted-foreground text-sm'>
                  {t('A verification code was sent to {{email}}.', {
                    email: flow.email,
                  })}
                </p>
                <div className='space-y-1.5'>
                  <div className='flex items-center justify-between gap-2'>
                    <Label htmlFor='wallet-password-code'>
                      {t('Email verification code')}
                    </Label>
                    <Button
                      type='button'
                      size='sm'
                      variant='ghost'
                      disabled={sending || isActive}
                      onClick={handleSendCode}
                    >
                      {resendAction}
                    </Button>
                  </div>
                  <Input
                    id='wallet-password-code'
                    autoComplete='one-time-code'
                    inputMode='numeric'
                    maxLength={6}
                    {...form.register('code')}
                  />
                  {errorText('code')}
                </div>
              </>
            )}

            {intent === 'change' ? (
              <div className='space-y-1.5'>
                <Label htmlFor='wallet-current-payment-password'>
                  {t('Current payment password')}
                </Label>
                <PasswordInput
                  id='wallet-current-payment-password'
                  autoComplete='off'
                  {...form.register('currentPassword')}
                />
                {errorText('currentPassword')}
              </div>
            ) : null}

            {intent === 'change' ? null : (
              <div className='space-y-1.5'>
                <Label htmlFor='wallet-reset-login-password'>
                  {t('Login password')}
                </Label>
                <PasswordInput
                  id='wallet-reset-login-password'
                  autoComplete='current-password'
                  {...form.register('loginPassword')}
                />
                {errorText('loginPassword')}
              </div>
            )}

            {intent === 'reset' && status?.two_fa_enabled ? (
              <div className='space-y-1.5'>
                <Label htmlFor='wallet-reset-2fa'>
                  {t('Two-factor authentication code')}
                </Label>
                <Input
                  id='wallet-reset-2fa'
                  autoComplete='one-time-code'
                  inputMode='numeric'
                  maxLength={32}
                  {...form.register('twoFACode')}
                />
                {errorText('twoFACode')}
              </div>
            ) : null}

            <div className='space-y-1.5'>
              <Label htmlFor='wallet-new-payment-password'>
                {t('New payment password')}
              </Label>
              <PasswordInput
                id='wallet-new-payment-password'
                autoComplete='new-password'
                {...form.register('newPassword')}
              />
              {errorText('newPassword')}
              <p className='text-muted-foreground text-xs'>
                {t(
                  'Use 8 to 128 characters, different from your login password. Common passwords are not accepted.'
                )}
              </p>
            </div>
            <div className='space-y-1.5'>
              <Label htmlFor='wallet-confirm-payment-password'>
                {t('Confirm new payment password')}
              </Label>
              <PasswordInput
                id='wallet-confirm-payment-password'
                autoComplete='new-password'
                {...form.register('confirmPassword')}
              />
              {errorText('confirmPassword')}
            </div>
          </form>
        ) : (
          <p className='text-muted-foreground text-sm'>{startPrompt}</p>
        )}
      </div>
    </Dialog>
  )
}
