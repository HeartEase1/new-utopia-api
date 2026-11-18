import { ExternalLink, Gift, Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Skeleton } from '@/components/ui/skeleton'
import { TitledCard } from '@/components/ui/titled-card'

import {
  isPurchaseUrl,
  parseRedemptionPurchase,
} from '../lib/redemption-purchase'
import type { TopupInfo } from '../types'

interface RedemptionCardProps {
  topupInfo: TopupInfo | null
  loading?: boolean
  code: string
  onCodeChange: (value: string) => void
  onRedeem: () => void
  redeeming: boolean
}

export function RedemptionCard(props: RedemptionCardProps) {
  const { t } = useTranslation()
  const purchase = parseRedemptionPurchase(props.topupInfo?.redemption_purchase)
  // Existing sites keep their legacy purchase link until the new settings are saved.
  const link = props.topupInfo?.redemption_purchase?.trim()
    ? purchase.url
    : props.topupInfo?.topup_link || ''
  const enabled = props.topupInfo?.enable_redemption !== false

  return (
    <TitledCard
      title={t('Redemption codes')}
      description={t('Buy a code or redeem one to add funds')}
      icon={<Gift className='size-4' />}
      iconTone='warning'
      disableHoverEffect
      contentClassName='space-y-5'
    >
      {props.loading && <Skeleton className='h-32 w-full' />}
      {!props.loading && !enabled && (
        <Alert>
          <AlertDescription>
            {t(
              'Redemption codes are disabled until the administrator confirms compliance terms.'
            )}
          </AlertDescription>
        </Alert>
      )}
      {!props.loading && enabled && (
        <>
          {isPurchaseUrl(link) && (
            <div className='bg-muted/30 space-y-3 rounded-lg border p-4'>
              <Button
                role='link'
                className='h-auto min-h-9 w-full py-2 break-words whitespace-normal'
                render={
                  <a href={link} target='_blank' rel='noopener noreferrer' />
                }
              >
                <ExternalLink className='size-4 shrink-0' aria-hidden='true' />
                {purchase.name.trim() || t('Buy a redemption code')}
              </Button>
              {(purchase.fee || purchase.payment_methods) && (
                <dl className='space-y-2 text-sm'>
                  {purchase.fee && (
                    <div>
                      <dt className='text-muted-foreground'>
                        {t('Fee description')}
                      </dt>
                      <dd className='break-words whitespace-pre-wrap'>
                        {purchase.fee}
                      </dd>
                    </div>
                  )}
                  {purchase.payment_methods && (
                    <div>
                      <dt className='text-muted-foreground'>
                        {t('Supported payment methods')}
                      </dt>
                      <dd className='break-words whitespace-pre-wrap'>
                        {purchase.payment_methods}
                      </dd>
                    </div>
                  )}
                </dl>
              )}
            </div>
          )}
          <form
            aria-label={t('Redeem')}
            className='space-y-2.5'
            onSubmit={(event) => {
              event.preventDefault()
              if (!props.redeeming && props.code.trim()) props.onRedeem()
            }}
          >
            <Label htmlFor='redemption-code'>{t('Have a Code?')}</Label>
            <Input
              id='redemption-code'
              value={props.code}
              onChange={(event) => props.onCodeChange(event.target.value)}
              placeholder={t('Enter your redemption code')}
              autoComplete='off'
              disabled={props.redeeming}
            />
            <Button
              type='submit'
              className='w-full'
              variant='outline'
              disabled={props.redeeming || !props.code.trim()}
            >
              {props.redeeming && (
                <Loader2 className='size-4 animate-spin' aria-hidden='true' />
              )}
              {t('Redeem')}
            </Button>
          </form>
        </>
      )}
    </TitledCard>
  )
}
