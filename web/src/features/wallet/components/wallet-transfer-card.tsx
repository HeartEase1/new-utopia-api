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
import { ArrowLeftRight, History, ReceiptText } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'

interface WalletTransferCardProps {
  onTransfer: () => void
  onHistory: () => void
  onManagePassword: () => void
  onReconciliation?: () => void
}

export function WalletTransferCard(props: WalletTransferCardProps) {
  const { t } = useTranslation()

  return (
    <Card data-card-hover='false' className='bg-muted/20 py-0'>
      <CardContent className='flex flex-col gap-3 p-3 sm:flex-row sm:items-center sm:justify-between sm:p-4'>
        <div className='flex min-w-0 items-center gap-3'>
          <div className='bg-primary/10 text-primary flex size-10 shrink-0 items-center justify-center rounded-lg'>
            <ArrowLeftRight className='size-5' />
          </div>
          <div className='min-w-0'>
            <h3 className='text-sm font-semibold'>{t('Balance transfer')}</h3>
            <p className='text-muted-foreground text-xs'>
              {t(
                'Transfer wallet balance to another user by entering their full username or user ID.'
              )}
            </p>
          </div>
        </div>
        <div className='flex flex-wrap gap-2'>
          <Button variant='outline' size='sm' onClick={props.onManagePassword}>
            {t('Payment password')}
          </Button>
          <Button variant='outline' size='sm' onClick={props.onHistory}>
            <History className='mr-2 size-4' />
            {t('Transfer history')}
          </Button>
          {props.onReconciliation && (
            <Button
              variant='outline'
              size='sm'
              onClick={props.onReconciliation}
            >
              <ReceiptText className='mr-2 size-4' />
              {t('Reconciliation')}
            </Button>
          )}
          <Button size='sm' onClick={props.onTransfer}>
            <ArrowLeftRight className='mr-2 size-4' />
            {t('Transfer balance')}
          </Button>
        </div>
      </CardContent>
    </Card>
  )
}
