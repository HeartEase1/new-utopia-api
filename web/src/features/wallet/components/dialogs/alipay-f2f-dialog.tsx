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
import { QRCodeSVG } from 'qrcode.react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { CopyButton } from '@/components/copy-button'
import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'

import { queryAlipayF2FOrder, type AlipayF2FOrder } from '../../api'
import { buildAlipayDeepLink, isAlipayLaunchRestricted } from '../../lib/alipay'

interface AlipayF2FDialogProps {
  order: AlipayF2FOrder
  onClose: () => void
  onPaid: () => void
}

// Existing Dialog and CopyButton own focus, keyboard and copy behavior. This
// component adds only payment-specific QR rendering and server status polling.
export function AlipayF2FDialog(props: AlipayF2FDialogProps) {
  const { t } = useTranslation()
  const [autoCheckEndsAt] = useState(() => Date.now() + 2 * 60 * 1000)
  const [autoCheckStopped, setAutoCheckStopped] = useState(false)
  const [expired, setExpired] = useState(
    () => props.order.expires_at * 1000 <= Date.now()
  )
  const query = useQuery({
    queryKey: ['alipay-f2f-order', props.order.trade_no],
    queryFn: ({ signal }) => queryAlipayF2FOrder(props.order.trade_no, signal),
    enabled: !expired && !autoCheckStopped,
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
    refetchInterval: (q) => {
      if (
        expired ||
        autoCheckStopped ||
        Date.now() >= autoCheckEndsAt ||
        q.state.error ||
        q.state.data === 'success' ||
        q.state.data === 'expired'
      ) {
        return false
      }
      return 5000
    },
    retry: false,
    meta: { errorToast: false },
  })
  const paid = query.data === 'success'
  const onPaid = props.onPaid
  const closed = query.data === 'expired'
  const deepLink = buildAlipayDeepLink(props.order.qr_code)
  const restricted = isAlipayLaunchRestricted(navigator.userAgent)

  useEffect(() => {
    if (paid) onPaid()
  }, [paid, onPaid])

  useEffect(() => {
    const timer = window.setTimeout(
      () => setAutoCheckStopped(true),
      Math.max(0, autoCheckEndsAt - Date.now())
    )
    return () => window.clearTimeout(timer)
  }, [autoCheckEndsAt])

  useEffect(() => {
    const timer = window.setTimeout(
      () => setExpired(true),
      Math.max(0, props.order.expires_at * 1000 - Date.now())
    )
    return () => window.clearTimeout(timer)
  }, [props.order.expires_at])

  return (
    <Dialog
      open
      disablePointerDismissal
      showCloseButton={false}
      onOpenChange={(_, details) => details.cancel()}
      title={t('Alipay payment')}
      description={t('Scan with Alipay or open the Alipay app on your phone.')}
      contentClassName='sm:max-w-md'
      footer={
        <Button variant='outline' onClick={props.onClose}>
          {t('Cancel')}
        </Button>
      }
    >
      <div className='flex flex-col items-center gap-4'>
        <p className='text-2xl font-semibold'>CNY {props.order.amount}</p>
        <div role='status' aria-live='polite'>
          {paid ? t('Payment received') : t('Waiting for payment confirmation')}
        </div>
        {!paid && !closed && !expired && props.order.qr_code && (
          <div className='rounded-lg bg-white p-4'>
            <QRCodeSVG
              value={props.order.qr_code}
              size={224}
              role='img'
              aria-label={t('Alipay payment QR code')}
            />
          </div>
        )}
        {!paid && !closed && !expired && deepLink && !restricted && (
          <Button
            onClick={() => {
              window.location.href = deepLink
            }}
          >
            {t('Open Alipay')}
          </Button>
        )}
        {!paid && (
          <>
            <p className='text-muted-foreground text-center text-sm'>
              {t(
                'If Alipay does not open, use your system browser or scan the QR code with Alipay.'
              )}
            </p>
            {(expired || closed) && (
              <p>
                {t(
                  'This QR code has expired. If you already paid, check the payment status.'
                )}
              </p>
            )}
            {autoCheckStopped && !expired && !closed && (
              <p className='text-muted-foreground text-center text-sm'>
                {t(
                  'Automatic checking has stopped. After paying, click Check payment status.'
                )}
              </p>
            )}
            {query.isError && (
              <p role='alert'>
                {t('Unable to check payment. Please retry; do not pay again.')}
              </p>
            )}
            <Button
              variant='outline'
              disabled={query.isFetching}
              onClick={() => void query.refetch()}
            >
              {query.isFetching
                ? t('Checking payment...')
                : t('Check payment status')}
            </Button>
          </>
        )}
        <div className='flex max-w-full items-center gap-2'>
          <span className='truncate font-mono text-xs'>
            {props.order.trade_no}
          </span>
          <CopyButton
            value={props.order.trade_no}
            aria-label={t('Copy order number')}
          />
        </div>
      </div>
    </Dialog>
  )
}
