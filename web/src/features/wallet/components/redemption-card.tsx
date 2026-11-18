import { ExternalLink, Gift, Loader2 } from 'lucide-react'
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Skeleton } from '@/components/ui/skeleton'
import { TitledCard } from '@/components/ui/titled-card'

import {
  isPurchaseUrl,
  parseRedemptionStores,
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
  const enabled = props.topupInfo?.enable_redemption !== false

  const stores = useMemo(() => {
    const raw = props.topupInfo?.redemption_purchase
    const storesConfigured = Array.isArray(raw) || !!raw?.trim()
    const legacy = props.topupInfo?.topup_link
    const parsed = parseRedemptionStores(raw)
    // Existing sites keep their legacy purchase link until the new settings are saved.
    const all =
      storesConfigured || !legacy
        ? parsed
        : [...parsed, { name: '', url: legacy, fee: '', payment_methods: '' }]
    return all.filter((store) => isPurchaseUrl(store.url))
  }, [props.topupInfo?.redemption_purchase, props.topupInfo?.topup_link])

  return (
    <TitledCard
      title={t('Redemption codes')}
      description={t('Enter a code to add funds')}
      icon={<Gift className='size-4' />}
      iconTone='warning'
      disableHoverEffect
      contentClassName='space-y-4'
    >
      {props.loading && <Skeleton className='h-28 w-full' />}
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
          <form
            aria-label={t('Redeem')}
            className='space-y-2'
            onSubmit={(event) => {
              event.preventDefault()
              if (!props.redeeming && props.code.trim()) props.onRedeem()
            }}
          >
            <Label htmlFor='redemption-code'>{t('Have a Code?')}</Label>
            <div className='flex gap-2'>
              <Input
                id='redemption-code'
                value={props.code}
                onChange={(event) => props.onCodeChange(event.target.value)}
                placeholder={t('Enter your redemption code')}
                autoComplete='off'
                className='min-w-0 flex-1'
                disabled={props.redeeming}
              />
              <Button
                type='submit'
                className='shrink-0'
                disabled={props.redeeming || !props.code.trim()}
              >
                {props.redeeming && (
                  <Loader2 className='size-4 animate-spin' aria-hidden='true' />
                )}
                {t('Redeem')}
              </Button>
            </div>
          </form>

          {stores.length > 0 && (
            <div className='space-y-2 border-t pt-4'>
              <p className='text-muted-foreground text-xs font-medium tracking-wider uppercase'>
                {t('Buy codes')}
              </p>
              <ul className='grid gap-2 sm:grid-cols-2 xl:grid-cols-1'>
                {stores.map((store) => (
                  <li key={store.url}>
                    <a
                      href={store.url}
                      target='_blank'
                      rel='noopener noreferrer'
                      className='group border-border hover:border-primary/50 hover:bg-muted/40 focus-visible:ring-ring/50 block rounded-lg border px-3 py-2.5 transition-colors outline-none focus-visible:ring-[3px]'
                    >
                      <span className='flex items-center justify-between gap-2'>
                        <span className='truncate text-sm font-medium'>
                          {store.name.trim() || t('Buy a redemption code')}
                        </span>
                        <ExternalLink
                          className='text-muted-foreground group-hover:text-primary size-3.5 shrink-0'
                          aria-hidden='true'
                        />
                      </span>
                      {(store.fee.trim() || store.payment_methods.trim()) && (
                        <span className='text-muted-foreground mt-1 line-clamp-2 block text-xs leading-5'>
                          {[store.fee.trim(), store.payment_methods.trim()]
                            .filter(Boolean)
                            .join(' · ')}
                        </span>
                      )}
                    </a>
                  </li>
                ))}
              </ul>
            </div>
          )}
        </>
      )}
    </TitledCard>
  )
}
