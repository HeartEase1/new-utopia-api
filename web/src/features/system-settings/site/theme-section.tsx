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
import { useMemo, type BaseSyntheticEvent } from 'react'
import { useTranslation } from 'react-i18next'
import * as z from 'zod'

import { ThemePresetPicker } from '@/components/theme-preset-picker'
import {
  Form,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { handleServerError } from '@/lib/handle-server-error'

import { FormDirtyIndicator } from '../components/form-dirty-indicator'
import { FormNavigationGuard } from '../components/form-navigation-guard'
import { SettingsForm } from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useSettingsForm } from '../hooks/use-settings-form'
import { useUpdateOption } from '../hooks/use-update-option'

const themeSchema = z.object({
  theme_setting: z.object({
    default_preset: z.string().min(1),
  }),
})

type ThemeFormValues = z.infer<typeof themeSchema>

type ThemeSectionProps = {
  defaultPreset: string
}

export function ThemeSection(props: ThemeSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const defaultValues = useMemo(
    () => ({
      theme_setting: { default_preset: props.defaultPreset || 'default' },
    }),
    [props.defaultPreset]
  )

  const { form, handleSubmit, handleReset, isDirty, isSubmitting } =
    useSettingsForm<ThemeFormValues>({
      resolver: zodResolver(themeSchema),
      defaultValues,
      onSubmit: async (_data, changedFields) => {
        for (const [key, value] of Object.entries(changedFields)) {
          await updateOption.mutateAsync({ key, value: String(value) })
        }
      },
    })

  const submit = (event?: BaseSyntheticEvent) => {
    void handleSubmit(event).catch(handleServerError)
  }

  return (
    <>
      <FormNavigationGuard when={isDirty} />

      <SettingsSection title={t('Site theme')}>
        <Form {...form}>
          <SettingsForm onSubmit={submit}>
            <SettingsPageFormActions
              onSave={submit}
              onReset={handleReset}
              isSaving={isSubmitting || updateOption.isPending}
              isResetDisabled={!isDirty}
            />
            <FormDirtyIndicator isDirty={isDirty} />
            <FormField
              control={form.control}
              name='theme_setting.default_preset'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Default theme')}</FormLabel>
                  <ThemePresetPicker
                    value={field.value}
                    onValueChange={field.onChange}
                    ariaLabel={t('Default theme')}
                    className='max-w-xl sm:grid-cols-5'
                  />
                  <FormDescription>
                    {t(
                      'Everyone who has not picked a theme follows this one, so saving switches them all at once. Users who picked their own theme keep it.'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          </SettingsForm>
        </Form>
      </SettingsSection>
    </>
  )
}
