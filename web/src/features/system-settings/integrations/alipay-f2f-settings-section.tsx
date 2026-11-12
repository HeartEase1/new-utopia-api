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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Save, ShieldCheck } from 'lucide-react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import * as z from 'zod'

import { CopyButton } from '@/components/copy-button'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { Textarea } from '@/components/ui/textarea'
import { handleServerError } from '@/lib/handle-server-error'
import { requireServerSuccess } from '@/lib/server-error-message'

import { SettingsSwitchField } from '../components/settings-form-layout'
import {
  checkAlipayF2FSettings,
  getAlipayF2FSettings,
  saveAlipayF2FSettings,
  type AlipayF2FSettings,
  type AlipayF2FSettingsInput,
} from './alipay-f2f-api'

const queryKey = ['system-settings', 'alipay-f2f'] as const

type FormValues = AlipayF2FSettingsInput

function initialValues(settings: AlipayF2FSettings): FormValues {
  return {
    enabled: settings.enabled,
    sandbox: settings.sandbox,
    app_id: settings.app_id,
    seller_id: settings.seller_id,
    notify_url: settings.notify_url,
    key_storage: settings.key_storage === 'encrypted' ? 'encrypted' : 'file',
    private_key: '',
    alipay_public_key: '',
    revision: settings.revision,
  }
}

const schema = z.object({
  enabled: z.boolean(),
  sandbox: z.boolean(),
  app_id: z.string().trim().min(1),
  seller_id: z.string().trim().min(1),
  notify_url: z
    .string()
    .trim()
    .refine((value) => {
      try {
        const url = new URL(value)
        return (
          url.protocol === 'https:' &&
          url.pathname === '/api/alipay-f2f/notify' &&
          !url.username &&
          !url.password &&
          !url.search &&
          !url.hash
        )
      } catch {
        return false
      }
    }),
  key_storage: z.enum(['file', 'encrypted']),
  private_key: z.string().trim(),
  alipay_public_key: z.string().trim(),
  revision: z.string(),
})

export function AlipayF2FSettingsSection() {
  const { t } = useTranslation()
  const query = useQuery({
    queryKey,
    queryFn: async () =>
      requireServerSuccess(await getAlipayF2FSettings()).data,
    refetchOnWindowFocus: false,
    retry: false,
  })
  if (query.isPending) return <LoadingState className='min-h-40' />
  if (query.isError || !query.data) {
    return (
      <ErrorState
        title={t('Unable to load Alipay settings.')}
        onRetry={() => void query.refetch()}
      />
    )
  }
  return (
    <AlipayF2FSettingsForm key={query.data.revision} settings={query.data} />
  )
}

function AlipayF2FSettingsForm({ settings }: { settings: AlipayF2FSettings }) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const form = useForm<FormValues>({
    resolver: zodResolver(schema),
    defaultValues: initialValues(settings),
  })
  const values = form.watch()
  const saveMutation = useMutation({
    mutationFn: saveAlipayF2FSettings,
    onSuccess: (response) => {
      try {
        const data = requireServerSuccess(response).data
        form.reset(initialValues(data))
        queryClient.setQueryData(queryKey, data)
        toast.success(t('Alipay F2F settings saved'))
      } catch (error) {
        handleServerError(error)
      }
    },
    onError: (error) => handleServerError(error),
  })
  const checkMutation = useMutation({
    mutationFn: checkAlipayF2FSettings,
    onSuccess: (response) => {
      try {
        requireServerSuccess(response)
        toast.success(
          t(
            'Alipay connection verified. Seller PID and payment callbacks still require a test payment.'
          )
        )
      } catch (error) {
        handleServerError(error)
      }
    },
    onError: (error) => handleServerError(error),
  })

  const submit = (operation: 'save' | 'check') =>
    form.handleSubmit(
      (input) => {
        if (operation === 'save') saveMutation.mutate(input)
        else checkMutation.mutate(input)
      },
      () =>
        toast.error(
          t('Check the Alipay app ID, seller PID and HTTPS callback URL.')
        )
    )()
  const busy = saveMutation.isPending || checkMutation.isPending

  return (
    <fieldset disabled={busy} className='space-y-6'>
      <div className='flex flex-wrap items-center justify-between gap-3'>
        <div>
          <h3 className='text-base font-medium'>{t('Alipay F2F')}</h3>
          <p className='text-muted-foreground text-sm'>
            {t('Configure Alipay face-to-face wallet top-ups.')}
          </p>
        </div>
        <div className='flex flex-wrap gap-2'>
          <Button
            type='button'
            variant='outline'
            onClick={() => void submit('check')}
            disabled={busy}
          >
            <ShieldCheck data-icon='inline-start' />
            {t('Check connection')}
          </Button>
          <Button
            type='button'
            onClick={() => void submit('save')}
            disabled={busy}
          >
            <Save data-icon='inline-start' />
            {saveMutation.isPending
              ? t('Saving...')
              : t('Save Alipay settings')}
          </Button>
        </div>
      </div>

      <div className='grid gap-5 md:grid-cols-2'>
        {(
          [
            [
              'enabled',
              t('Enable Alipay F2F top-ups'),
              t('New Alipay top-up orders are available to users.'),
            ],
            [
              'sandbox',
              t('Alipay sandbox'),
              t('Use the Alipay sandbox gateway for testing.'),
            ],
          ] as const
        ).map(([key, label, description]) => (
          <SettingsSwitchField
            key={key}
            controlId={`alipay-${key}`}
            label={label}
            description={description}
            checked={values[key]}
            onCheckedChange={(checked) => form.setValue(key, checked)}
            className='md:col-span-2'
          />
        ))}
        <div className='space-y-2'>
          <Label htmlFor='alipay-app-id'>{t('Alipay app ID')}</Label>
          <Input
            id='alipay-app-id'
            autoComplete='off'
            aria-invalid={!!form.formState.errors.app_id}
            {...form.register('app_id')}
          />
        </div>
        <div className='space-y-2'>
          <Label htmlFor='alipay-seller-id'>{t('Merchant PID')}</Label>
          <Input
            id='alipay-seller-id'
            autoComplete='off'
            aria-invalid={!!form.formState.errors.seller_id}
            {...form.register('seller_id')}
          />
        </div>
        <div className='space-y-2 md:col-span-2'>
          <Label htmlFor='alipay-notify-url'>{t('Payment callback URL')}</Label>
          <div className='flex gap-2'>
            <Input
              id='alipay-notify-url'
              aria-invalid={!!form.formState.errors.notify_url}
              {...form.register('notify_url')}
            />
            <CopyButton
              value={values.notify_url}
              tooltip={t('Copy callback URL')}
            />
          </div>
          <p className='text-muted-foreground text-sm'>
            {t(
              'Set the same URL as the asynchronous notification address in Alipay Open Platform.'
            )}
          </p>
        </div>
        <div className='space-y-2 md:col-span-2'>
          <Label htmlFor='alipay-private-key'>
            {t('Alipay application private key')}
          </Label>
          <Textarea
            id='alipay-private-key'
            autoComplete='off'
            spellCheck={false}
            rows={5}
            placeholder={
              settings.private_key_configured
                ? t('Already configured; leave blank to keep the current key.')
                : '-----BEGIN PRIVATE KEY-----'
            }
            {...form.register('private_key')}
          />
          <p className='text-muted-foreground text-sm'>
            {t('The key is not shown again after saving.')}
          </p>
        </div>
        <div className='space-y-3 md:col-span-2'>
          <Label id='alipay-key-storage-label'>
            {t('Private key storage')}
          </Label>
          {settings.private_key_unavailable && (
            <Alert variant='destructive'>
              <AlertDescription>
                {t(
                  'This server cannot read the saved private key. Enter the private key again and save.'
                )}
              </AlertDescription>
            </Alert>
          )}
          {settings.key_storage === 'database' && (
            <Alert>
              <AlertDescription>
                {t(
                  'The private key is currently stored unencrypted in the database. Save to move it to the selected storage.'
                )}
              </AlertDescription>
            </Alert>
          )}
          <RadioGroup
            aria-labelledby='alipay-key-storage-label'
            value={values.key_storage}
            onValueChange={(value) => {
              if (value === 'file' || value === 'encrypted') {
                form.setValue('key_storage', value)
              }
            }}
          >
            {(
              [
                [
                  'file',
                  t('Server file'),
                  t(
                    'The key is written to the server data directory and is not stored in the database. Suited to a single server.'
                  ),
                ],
                [
                  'encrypted',
                  t('Encrypted in database'),
                  t(
                    'The key is encrypted with CRYPTO_SECRET before it is stored in the database. Suited to multiple servers.'
                  ),
                ],
              ] as const
            ).map(([storage, label, description]) => (
              <div key={storage} className='flex items-start gap-2'>
                <RadioGroupItem
                  id={`alipay-key-storage-${storage}`}
                  value={storage}
                  className='mt-0.5'
                  aria-describedby={`alipay-key-storage-${storage}-description`}
                  disabled={
                    storage === 'encrypted' && !settings.encryption_available
                  }
                />
                <div className='space-y-0.5'>
                  <Label htmlFor={`alipay-key-storage-${storage}`}>
                    {label}
                  </Label>
                  <p
                    id={`alipay-key-storage-${storage}-description`}
                    className='text-muted-foreground text-sm'
                  >
                    {description}
                  </p>
                </div>
              </div>
            ))}
          </RadioGroup>
          {!settings.encryption_available && (
            <p className='text-muted-foreground text-sm'>
              {t(
                'Set the CRYPTO_SECRET environment variable to enable encrypted storage.'
              )}
            </p>
          )}
        </div>
        <div className='space-y-2 md:col-span-2'>
          <Label htmlFor='alipay-public-key'>{t('Alipay public key')}</Label>
          <Textarea
            id='alipay-public-key'
            autoComplete='off'
            spellCheck={false}
            rows={5}
            placeholder={
              settings.alipay_public_key_configured
                ? t('Already configured; leave blank to keep the current key.')
                : '-----BEGIN PUBLIC KEY-----'
            }
            {...form.register('alipay_public_key')}
          />
          <p className='text-muted-foreground text-sm'>
            {t(
              'Use the Alipay public key in RSA2 public-key mode, not a certificate.'
            )}
          </p>
        </div>
      </div>
      {settings.from_environment && (
        <p className='text-muted-foreground text-sm'>
          {t(
            'Loaded from existing environment settings. Saving here switches to settings managed on this page.'
          )}
        </p>
      )}
    </fieldset>
  )
}
