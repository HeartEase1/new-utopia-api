import { useId } from 'react'
import { useTranslation } from 'react-i18next'

import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { parseRedemptionPurchase } from '@/features/wallet/lib/redemption-purchase'

interface RedemptionPurchaseEditorProps {
  value: string
  onChange: (value: string) => void
}

export function RedemptionPurchaseEditor(props: RedemptionPurchaseEditorProps) {
  const { t } = useTranslation()
  const id = useId()
  const purchase = parseRedemptionPurchase(props.value)
  const fields = [
    { key: 'name', label: t('Purchase button name'), limit: 80 },
    { key: 'url', label: t('Purchase link'), limit: 2048 },
    { key: 'fee', label: t('Fee description'), limit: 200 },
    {
      key: 'payment_methods',
      label: t('Supported payment methods'),
      limit: 200,
    },
  ] as const

  return (
    <div className='grid gap-3'>
      {fields.map((field) => (
        <div key={field.key} className='space-y-1.5'>
          <Label htmlFor={`${id}-${field.key}`}>{field.label}</Label>
          <Input
            id={`${id}-${field.key}`}
            value={purchase[field.key]}
            maxLength={field.limit}
            type={field.key === 'url' ? 'url' : 'text'}
            onChange={(event) =>
              props.onChange(
                JSON.stringify({
                  ...purchase,
                  [field.key]: event.target.value,
                })
              )
            }
          />
        </div>
      ))}
    </div>
  )
}
