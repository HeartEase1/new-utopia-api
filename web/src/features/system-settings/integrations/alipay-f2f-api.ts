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
import { api } from '@/lib/api'

export type AlipayF2FSettings = {
  enabled: boolean
  sandbox: boolean
  notify_alipay_app: boolean
  app_id: string
  seller_id: string
  notify_url: string
  private_key_configured: boolean
  alipay_public_key_configured: boolean
  revision: string
  from_environment: boolean
  // '' before the first save; 'database' is the legacy unencrypted storage.
  key_storage: '' | 'file' | 'encrypted' | 'database'
  private_key_unavailable: boolean
  encryption_available: boolean
}

export type AlipayF2FKeyStorage = 'file' | 'encrypted'

export type AlipayF2FSettingsInput = Pick<
  AlipayF2FSettings,
  | 'enabled'
  | 'sandbox'
  | 'notify_alipay_app'
  | 'app_id'
  | 'seller_id'
  | 'notify_url'
> & {
  key_storage: AlipayF2FKeyStorage
  private_key: string
  alipay_public_key: string
  revision: string
}

export type AlipayF2FSettingsResponse = {
  success: boolean
  message: string
  data: AlipayF2FSettings
}

export async function getAlipayF2FSettings() {
  const response = await api.get<AlipayF2FSettingsResponse>(
    '/api/option/alipay-f2f'
  )
  return response.data
}

export async function saveAlipayF2FSettings(input: AlipayF2FSettingsInput) {
  const response = await api.put<AlipayF2FSettingsResponse>(
    '/api/option/alipay-f2f',
    input
  )
  return response.data
}

export async function checkAlipayF2FSettings(input: AlipayF2FSettingsInput) {
  const response = await api.post<AlipayF2FSettingsResponse>(
    '/api/option/alipay-f2f/check',
    input
  )
  return response.data
}
