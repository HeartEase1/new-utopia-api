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
import {
  AlertTriangle,
  ChevronLeft,
  ReceiptText,
  ShieldCheck,
} from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ErrorState } from '@/components/error-state'
import { SectionPageLayout } from '@/components/layout'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { getWalletTransferStatus } from '@/features/wallet/api'
import { useIsAdmin } from '@/hooks/use-admin'
import { getSelf } from '@/lib/api'
import { formatQuota } from '@/lib/format'
import { requireServerSuccess } from '@/lib/server-error-message'

import { PaymentPasswordDialog } from './components/payment-password-dialog'
import { TransferForm } from './components/transfer-form'
import {
  TransferHistory,
  TransferReconciliationDialog,
} from './components/transfer-history'
import { TransferNotes, TransferOverview } from './components/transfer-overview'

export function WalletTransfer() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const isAdmin = useIsAdmin()
  const [passwordOpen, setPasswordOpen] = useState(false)
  const [reconciliationOpen, setReconciliationOpen] = useState(false)

  const statusQuery = useQuery({
    queryKey: ['wallet-transfer-status'],
    queryFn: getWalletTransferStatus,
    staleTime: 0,
    meta: { errorToast: false },
  })
  const selfQuery = useQuery({
    queryKey: ['wallet-transfer-self'],
    queryFn: async () => requireServerSuccess(await getSelf()),
    staleTime: 0,
    meta: { errorToast: false },
  })
  const status = statusQuery.data
  const self = selfQuery.data?.data
  const availableQuota = self?.quota

  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: ['wallet-transfer-status'] })
    void queryClient.invalidateQueries({ queryKey: ['wallet-transfer-self'] })
    void queryClient.invalidateQueries({
      queryKey: ['wallet-transfer-history'],
    })
  }

  const emailReady = status?.has_email === true || status?.email_exempt === true

  return (
    <>
      <SectionPageLayout>
        <SectionPageLayout.Breadcrumb>
          <Button
            variant='ghost'
            size='sm'
            className='-ml-2'
            render={<Link to='/wallet' />}
          >
            <ChevronLeft data-icon='inline-start' />
            {t('Back to wallet')}
          </Button>
        </SectionPageLayout.Breadcrumb>
        <SectionPageLayout.Title>
          {t('Balance transfer')}
        </SectionPageLayout.Title>
        <SectionPageLayout.Actions>
          {isAdmin ? (
            <Button
              variant='outline'
              size='sm'
              onClick={() => setReconciliationOpen(true)}
            >
              <ReceiptText data-icon='inline-start' />
              {t('Site-wide reconciliation')}
            </Button>
          ) : null}
        </SectionPageLayout.Actions>
        <SectionPageLayout.Content>
          <div className='mx-auto w-full max-w-6xl space-y-4'>
            {statusQuery.isError ? (
              <ErrorState
                className='min-h-40'
                title={t('Could not load transfer security settings.')}
                onRetry={() => void statusQuery.refetch()}
              />
            ) : null}

            {status?.limits_exempt ? (
              <Alert>
                <ShieldCheck />
                <AlertTitle>
                  {t('No transfer limits for the super administrator')}
                </AlertTitle>
                <AlertDescription>
                  {status.two_fa_enabled
                    ? t(
                        'Your transfers have no daily amount cap and no attempt limit.'
                      )
                    : t(
                        'Your transfers have no daily amount cap and no attempt limit. Enabling 2FA is still recommended to protect your balance.'
                      )}
                  {status.two_fa_enabled ? null : (
                    <Button
                      size='sm'
                      variant='outline'
                      className='mt-2'
                      render={<Link to='/security' />}
                    >
                      {t('Enable 2FA')}
                    </Button>
                  )}
                </AlertDescription>
              </Alert>
            ) : null}
            {status && !status.limits_exempt && status.two_fa_enabled ? (
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
            ) : null}
            {status && !status.limits_exempt && !status.two_fa_enabled ? (
              <Alert>
                <AlertTriangle />
                <AlertTitle>
                  {t('Enable 2FA for higher transfer limits')}
                </AlertTitle>
                <AlertDescription>
                  {t(
                    'Without 2FA, you can transfer up to {{amount}} per day and start up to {{count}} transfers. Enabling 2FA removes the daily amount cap and raises the attempt limit to 100.',
                    {
                      amount: formatQuota(status.daily_limit_quota),
                      count: status.attempt_limit,
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
            ) : null}

            <div className='grid items-start gap-4 lg:grid-cols-[minmax(0,1fr)_22rem]'>
              <div className='min-w-0 space-y-4'>
                <Card>
                  <CardHeader>
                    <CardTitle>{t('Transfer balance')}</CardTitle>
                    <CardDescription>
                      {t(
                        'Send part of your wallet balance to another user of this site.'
                      )}
                    </CardDescription>
                  </CardHeader>
                  <CardContent className='space-y-4'>
                    {status && !status.has_login_password ? (
                      <Alert variant='destructive'>
                        <AlertTriangle />
                        <AlertTitle>
                          {t('A login password is required')}
                        </AlertTitle>
                        <AlertDescription>
                          {t(
                            'Set a login password in your account security settings before transferring balance.'
                          )}
                        </AlertDescription>
                      </Alert>
                    ) : null}
                    {status && !status.has_payment_password ? (
                      <Alert>
                        <AlertTriangle />
                        <AlertTitle>
                          {t('Payment password required')}
                        </AlertTitle>
                        <AlertDescription>
                          {emailReady
                            ? t(
                                'Transfers are authorized with a separate payment password. Set one to start.'
                              )
                            : t(
                                'Bind an email address before setting a payment password.'
                              )}
                          <div className='mt-2'>
                            {emailReady ? (
                              <Button
                                size='sm'
                                onClick={() => setPasswordOpen(true)}
                              >
                                {t('Set payment password')}
                              </Button>
                            ) : (
                              <Button
                                size='sm'
                                variant='outline'
                                render={<Link to='/security' />}
                              >
                                {t('Bind email')}
                              </Button>
                            )}
                          </div>
                        </AlertDescription>
                      </Alert>
                    ) : null}
                    {status &&
                    !status.limits_exempt &&
                    status.attempt_count >= status.attempt_limit ? (
                      <Alert variant='destructive'>
                        <AlertTriangle />
                        <AlertDescription>
                          {t(
                            'You have reached today’s transfer attempt limit.'
                          )}
                        </AlertDescription>
                      </Alert>
                    ) : null}

                    <TransferForm
                      status={status}
                      availableQuota={availableQuota ?? 0}
                      onAttempted={refresh}
                    />
                  </CardContent>
                </Card>
                <TransferNotes />
              </div>

              <TransferOverview
                status={status}
                availableQuota={availableQuota}
                onManagePassword={() => setPasswordOpen(true)}
              />
            </div>

            <Card>
              <CardHeader>
                <CardTitle>{t('Transfer history')}</CardTitle>
                <CardDescription>
                  {t(
                    'Transfers you sent or received, with the amount, fee and credited amount.'
                  )}
                </CardDescription>
              </CardHeader>
              <CardContent>
                <TransferHistory currentUserId={self?.id ?? 0} />
              </CardContent>
            </Card>
          </div>
        </SectionPageLayout.Content>
      </SectionPageLayout>

      {/* Outside the layout: it renders only its own slots and drops other children. */}
      <PaymentPasswordDialog
        open={passwordOpen}
        onOpenChange={setPasswordOpen}
      />
      {isAdmin ? (
        <TransferReconciliationDialog
          open={reconciliationOpen}
          onOpenChange={setReconciliationOpen}
          currentUserId={self?.id ?? 0}
        />
      ) : null}
    </>
  )
}
