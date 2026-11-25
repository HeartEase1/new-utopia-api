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
import { ArrowLeftRight, ChevronRight } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'

/** Entry to the balance transfer page. */
export function WalletTransferCard() {
  const { t } = useTranslation()

  return (
    <Card
      data-card-hover='false'
      className='border-primary/30 bg-primary/5 py-0'
    >
      <CardContent className='flex flex-col gap-3 p-3 sm:flex-row sm:items-center sm:justify-between sm:p-4'>
        <div className='flex min-w-0 items-center gap-3'>
          <div className='bg-primary/10 text-primary flex size-10 shrink-0 items-center justify-center rounded-lg'>
            <ArrowLeftRight className='size-5' aria-hidden='true' />
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
        <Button size='sm' render={<Link to='/wallet/transfer' />}>
          {t('Open balance transfer')}
          <ChevronRight data-icon='inline-end' />
        </Button>
      </CardContent>
    </Card>
  )
}
