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
import { zodResolver } from '@hookform/resolvers/zod'
import { useEffect } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import * as z from 'zod'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import {
  isPurchaseUrl,
  type RedemptionStore,
} from '@/features/wallet/lib/redemption-purchase'

const STORE_FORM_ID = 'redemption-store-form'

const createStoreSchema = (t: (key: string) => string) =>
  z.object({
    name: z.string().max(80, t('Invalid redemption purchase settings')),
    url: z
      .string()
      .max(2048, t('Invalid redemption purchase settings'))
      .refine(
        (value) => value === '' || isPurchaseUrl(value),
        t('Purchase link must be an HTTP or HTTPS URL without credentials')
      ),
    fee: z.string().max(200, t('Invalid redemption purchase settings')),
    payment_methods: z
      .string()
      .max(200, t('Invalid redemption purchase settings')),
  })

type StoreFormValues = z.infer<ReturnType<typeof createStoreSchema>>

type RedemptionStoreDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  onSave: (store: RedemptionStore) => void
  editData?: RedemptionStore | null
}

export function RedemptionStoreDialog(props: RedemptionStoreDialogProps) {
  const { t } = useTranslation()
  const isEditMode = !!props.editData

  const form = useForm<StoreFormValues>({
    resolver: zodResolver(createStoreSchema(t)),
    defaultValues: { name: '', url: '', fee: '', payment_methods: '' },
  })

  useEffect(() => {
    if (!props.open) return
    form.reset(
      props.editData ?? { name: '', url: '', fee: '', payment_methods: '' }
    )
  }, [props.editData, props.open, form])

  const handleSubmit = (values: StoreFormValues) => {
    props.onSave({
      name: values.name.trim(),
      url: values.url.trim(),
      fee: values.fee.trim(),
      payment_methods: values.payment_methods.trim(),
    })
    props.onOpenChange(false)
  }

  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={isEditMode ? t('Edit store') : t('Add store')}
      description={t(
        'Each store is shown as a purchase button in the wallet. Leave the link empty to keep a store hidden.'
      )}
      contentClassName='sm:max-w-[520px]'
      contentHeight='auto'
      bodyClassName='space-y-4'
      footer={
        <>
          <Button
            type='button'
            variant='outline'
            onClick={() => props.onOpenChange(false)}
          >
            {t('Cancel')}
          </Button>
          <Button type='submit' form={STORE_FORM_ID}>
            {isEditMode ? t('Update') : t('Add')}
          </Button>
        </>
      }
    >
      <Form {...form}>
        <form
          id={STORE_FORM_ID}
          onSubmit={(event) => {
            // The dialog can live inside a settings form; keep its submit local.
            event.stopPropagation()
            void form.handleSubmit(handleSubmit)(event)
          }}
          className='space-y-4'
        >
          <FormField
            control={form.control}
            name='name'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Purchase button name')}</FormLabel>
                <FormControl>
                  <Input
                    placeholder={t('Buy a redemption code')}
                    maxLength={80}
                    {...field}
                  />
                </FormControl>
                <FormDescription>
                  {t('Shown on the button that opens the store.')}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name='url'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Purchase link')}</FormLabel>
                <FormControl>
                  <Input
                    type='url'
                    placeholder='https://store.example/codes'
                    maxLength={2048}
                    {...field}
                  />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          <div className='grid gap-4 sm:grid-cols-2'>
            <FormField
              control={form.control}
              name='fee'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Fee description')}</FormLabel>
                  <FormControl>
                    <Input placeholder='2%' maxLength={200} {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='payment_methods'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Supported payment methods')}</FormLabel>
                  <FormControl>
                    <Input
                      placeholder={t('Alipay / WeChat Pay')}
                      maxLength={200}
                      {...field}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>
        </form>
      </Form>
    </Dialog>
  )
}
