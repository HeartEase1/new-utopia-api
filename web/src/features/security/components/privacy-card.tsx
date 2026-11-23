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
import { useTranslation } from 'react-i18next'

import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { TitledCard } from '@/components/ui/titled-card'

export function PrivacyCard() {
  const { t } = useTranslation()

  return (
    <TitledCard
      title={t('Record IP Address')}
      description={t(
        'IP addresses are always recorded for usage and error logs'
      )}
      disableHoverEffect
    >
      <div className='flex items-center justify-between gap-4'>
        <Label htmlFor='security-record-ip'>{t('Record IP Address')}</Label>
        <Switch id='security-record-ip' checked disabled />
      </div>
    </TitledCard>
  )
}
