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

import { SidebarFooter } from '@/components/ui/sidebar'
import { useThemeCustomization } from '@/context/theme-customization-provider'
import { MASCOT_ART_SOURCE_URL, MASCOT_THEMES } from '@/lib/mascot-themes'

/**
 * Sidebar footer card for the character themes. Renders nothing under any
 * other preset, and hides while the desktop sidebar is collapsed to icons
 * (the mobile drawer always shows it).
 */
export function MascotSidebarCard() {
  const { t } = useTranslation()
  const { customization } = useThemeCustomization()
  const mascot = MASCOT_THEMES[customization.preset]

  if (!mascot) return null

  return (
    <SidebarFooter className='group-data-[collapsible=icon]:hidden'>
      <div className='bg-card flex flex-col gap-2 rounded-xl border p-2.5 shadow-xs'>
        <div className='flex items-center gap-2.5'>
          <img
            src={mascot.avatar}
            alt=''
            aria-hidden='true'
            width={48}
            height={48}
            className='bg-accent size-12 shrink-0 rounded-lg object-cover'
          />
          <div className='flex min-w-0 flex-col gap-0.5'>
            <p className='text-primary line-clamp-2 text-[0.625rem] leading-tight font-semibold tracking-normal uppercase'>
              {t(mascot.eyebrowKey)}
            </p>
            <p className='line-clamp-3 text-xs leading-snug font-medium'>
              {t(mascot.quoteKey)}
            </p>
          </div>
        </div>
        <a
          href={MASCOT_ART_SOURCE_URL}
          target='_blank'
          rel='noreferrer'
          className='text-muted-foreground hover:text-foreground line-clamp-2 text-[0.625rem] leading-snug underline-offset-2 hover:underline'
        >
          {t('Character art: {{credit}}', { credit: mascot.credit })}
        </a>
      </div>
    </SidebarFooter>
  )
}
