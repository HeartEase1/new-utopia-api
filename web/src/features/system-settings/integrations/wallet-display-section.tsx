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
import { useForm, type Resolver } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'

import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import {
  redemptionStoreListSchema,
  serializeRedemptionStores,
  type RedemptionStore,
} from '@/features/wallet/lib/redemption-purchase'

import {
  SettingsForm,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'
import { RedemptionStoresEditor } from './redemption-stores-editor'

const WALLET_ANNOUNCEMENT_KEY = 'payment_setting.wallet_announcement'
const REDEMPTION_PURCHASE_KEY = 'payment_setting.redemption_purchase'
const SHOW_SUBSCRIPTION_PLANS_KEY = 'payment_setting.show_subscription_plans'

type WalletDisplayValues = {
  announcement: string
  redemptionStores: string
  showSubscriptionPlans: boolean
}

export type WalletDisplayDefaults = WalletDisplayValues

const schema = z.object({
  announcement: z
    .string()
    .max(2000, 'Wallet announcement must not exceed 2000 characters'),
  redemptionStores: z.string().superRefine((value, ctx) => {
    if (!value.trim()) return
    try {
      const parsed: unknown = JSON.parse(value)
      const result = redemptionStoreListSchema.safeParse(parsed)
      if (!result.success) {
        ctx.addIssue({
          code: 'custom',
          message:
            result.error.issues[0]?.message ??
            'Invalid redemption purchase settings',
        })
      }
    } catch {
      ctx.addIssue({
        code: 'custom',
        message: 'Invalid redemption purchase settings',
      })
    }
  }),
  showSubscriptionPlans: z.boolean(),
})

export function WalletDisplaySection(props: {
  defaultValues: WalletDisplayDefaults
}) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()

  const form = useForm<z.infer<typeof schema>>({
    resolver: zodResolver(schema) as Resolver<z.infer<typeof schema>>,
    defaultValues: props.defaultValues,
  })

  const { isDirty, isSubmitting } = form.formState

  async function onSubmit(values: z.infer<typeof schema>) {
    const nextStores = serializeRedemptionStores(
      normalizeStores(values.redemptionStores)
    )
    const updates: Array<{ key: string; value: string }> = []

    if (values.announcement.trim() !== props.defaultValues.announcement) {
      updates.push({
        key: WALLET_ANNOUNCEMENT_KEY,
        value: values.announcement.trim(),
      })
    }
    if (nextStores !== props.defaultValues.redemptionStores) {
      updates.push({ key: REDEMPTION_PURCHASE_KEY, value: nextStores })
    }
    if (
      values.showSubscriptionPlans !== props.defaultValues.showSubscriptionPlans
    ) {
      updates.push({
        key: SHOW_SUBSCRIPTION_PLANS_KEY,
        value: String(values.showSubscriptionPlans),
      })
    }

    if (updates.length === 0) {
      toast.info(t('No changes to save'))
      return
    }

    for (const update of updates) {
      await updateOption.mutateAsync(update)
    }
    form.reset(values)
  }

  return (
    <SettingsSection title={t('Wallet Display')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={updateOption.isPending || isSubmitting}
            isSaveDisabled={!isDirty}
            saveLabel='Save wallet display settings'
          />

          <FormField
            control={form.control}
            name='announcement'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Wallet announcement')}</FormLabel>
                <FormControl>
                  <Textarea
                    {...field}
                    maxLength={2000}
                    rows={4}
                    placeholder={t(
                      'Optional notice shown above wallet amount options'
                    )}
                  />
                </FormControl>
                <FormDescription>
                  {t(
                    'Long notices scroll inside a single line in the wallet, pause on hover, and can be dragged by hand. Leave empty to hide the notice.'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <div className='space-y-2'>
            <Label>{t('Redemption code purchase')}</Label>
            <FormField
              control={form.control}
              name='redemptionStores'
              render={({ field }) => (
                <FormItem>
                  <FormControl>
                    <RedemptionStoresEditor
                      value={field.value}
                      onChange={field.onChange}
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'Each store becomes a purchase button in the wallet. Fees and payment methods are display information; the store handles payment. A store without a link stays hidden.'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>

          <FormField
            control={form.control}
            name='showSubscriptionPlans'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Show subscription plans')}</FormLabel>
                  <FormDescription>
                    {t(
                      'Turn off to remove the subscription panel from the wallet. Users with an active subscription can still see it from the subscription page.'
                    )}
                  </FormDescription>
                </SettingsSwitchContent>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                  />
                </FormControl>
              </SettingsSwitchItem>
            )}
          />
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}

function normalizeStores(value: string): RedemptionStore[] {
  if (!value.trim()) return []
  try {
    const parsed: unknown = JSON.parse(value)
    const result = redemptionStoreListSchema.safeParse(parsed)
    return result.success ? result.data : []
  } catch {
    return []
  }
}
