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
import { ExternalLink, Plus } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { StaticRowActions } from '@/components/data-table/static/static-row-actions'
import { Button } from '@/components/ui/button'
import {
  isPurchaseUrl,
  MAX_REDEMPTION_STORES,
  parseRedemptionStores,
  serializeRedemptionStores,
  type RedemptionStore,
} from '@/features/wallet/lib/redemption-purchase'
import { cn } from '@/lib/utils'

import { RedemptionStoreDialog } from './redemption-store-dialog'

type RedemptionStoresEditorProps = {
  value: string
  onChange: (value: string) => void
}

export function RedemptionStoresEditor(props: RedemptionStoresEditorProps) {
  const { t } = useTranslation()
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editIndex, setEditIndex] = useState<number | null>(null)

  const stores = useMemo(
    () => parseRedemptionStores(props.value),
    [props.value]
  )

  // Duplicate rows are allowed while editing, so keys carry an occurrence counter.
  const keyedStores = useMemo(() => {
    const seen = new Map<string, number>()
    return stores.map((store, index) => {
      const base = `${store.name}\u0000${store.url}`
      const occurrence = seen.get(base) ?? 0
      seen.set(base, occurrence + 1)
      return { store, index, key: `${base}\u0000${occurrence}` }
    })
  }, [stores])

  const saveStore = (store: RedemptionStore) => {
    const next = [...stores]
    if (editIndex === null) {
      next.push(store)
    } else {
      next[editIndex] = store
    }
    props.onChange(serializeRedemptionStores(next))
  }

  const removeStore = (index: number) => {
    props.onChange(
      serializeRedemptionStores(stores.filter((_, i) => i !== index))
    )
  }

  const canAdd = stores.length < MAX_REDEMPTION_STORES

  return (
    <div className='space-y-3'>
      {stores.length === 0 ? (
        <div className='text-muted-foreground rounded-lg border border-dashed p-6 text-center text-sm'>
          {t(
            'No stores configured. Add a store to show a purchase button in the wallet.'
          )}
        </div>
      ) : (
        <ul className='divide-y rounded-lg border'>
          {keyedStores.map(({ store, index, key }) => {
            const linked = isPurchaseUrl(store.url)
            return (
              <li key={key} className='flex items-start gap-3 p-3'>
                <div className='min-w-0 flex-1'>
                  <div className='flex flex-wrap items-center gap-2'>
                    <span className='truncate text-sm font-medium'>
                      {store.name.trim() || t('Buy a redemption code')}
                    </span>
                    {!linked && (
                      <span className='bg-muted text-muted-foreground rounded px-1.5 py-0.5 text-[11px]'>
                        {t('Hidden: no purchase link')}
                      </span>
                    )}
                  </div>
                  {linked && (
                    <a
                      href={store.url}
                      target='_blank'
                      rel='noopener noreferrer'
                      className='text-muted-foreground hover:text-foreground mt-1 inline-flex max-w-full items-center gap-1 text-xs break-all'
                    >
                      <span className='truncate'>{store.url}</span>
                      <ExternalLink className='size-3 shrink-0' />
                    </a>
                  )}
                  {(store.fee.trim() || store.payment_methods.trim()) && (
                    <p className={cn('text-muted-foreground mt-1 text-xs')}>
                      {[store.fee.trim(), store.payment_methods.trim()]
                        .filter(Boolean)
                        .join(' · ')}
                    </p>
                  )}
                </div>
                <StaticRowActions
                  editLabel={t('Edit')}
                  deleteLabel={t('Delete')}
                  menuLabel={t('Open menu')}
                  onEdit={() => {
                    setEditIndex(index)
                    setDialogOpen(true)
                  }}
                  onDelete={() => removeStore(index)}
                />
              </li>
            )
          })}
        </ul>
      )}

      <Button
        type='button'
        variant='outline'
        size='sm'
        disabled={!canAdd}
        onClick={() => {
          setEditIndex(null)
          setDialogOpen(true)
        }}
      >
        <Plus className='mr-2 h-3.5 w-3.5' />
        {t('Add store')}
      </Button>

      <RedemptionStoreDialog
        open={dialogOpen}
        onOpenChange={setDialogOpen}
        onSave={saveStore}
        editData={editIndex === null ? null : stores[editIndex]}
      />
    </div>
  )
}
