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

import { useThemeCustomization } from '@/context/theme-customization-provider'
import { useUserDisplay } from '@/hooks/use-user-display'
import { MASCOT_ART_SOURCE_URL, MASCOT_THEMES } from '@/lib/mascot-themes'
import { useAuthStore } from '@/stores/auth-store'

/**
 * Welcome banner for the character themes. Renders nothing under any other
 * preset. The greeting is a paragraph, not a heading, so the page outline
 * stays the same across themes.
 */
export function MascotBanner() {
  const { t } = useTranslation()
  const { customization } = useThemeCustomization()
  const user = useAuthStore((state) => state.auth.user)
  const display = useUserDisplay(user)
  const mascot = MASCOT_THEMES[customization.preset]

  if (!mascot) return null

  return (
    <section
      aria-label={t(mascot.eyebrowKey)}
      className='bg-card relative isolate flex min-h-28 items-center overflow-hidden rounded-2xl border shadow-xs sm:min-h-40'
    >
      <div
        aria-hidden='true'
        className='pointer-events-none absolute inset-0 -z-20 bg-[linear-gradient(100deg,color-mix(in_oklch,var(--chart-2)_14%,var(--card))_0%,var(--card)_45%,color-mix(in_oklch,var(--primary)_16%,var(--card))_100%)]'
      />
      <img
        src={mascot.portrait}
        alt=''
        aria-hidden='true'
        className='pointer-events-none absolute inset-y-0 right-28 -z-10 hidden h-full w-80 [mask-image:linear-gradient(to_right,transparent,black_40%,black_75%,transparent)] object-cover object-[center_38%] md:block lg:w-96 dark:opacity-80'
      />
      <img
        src={mascot.idle}
        alt=''
        aria-hidden='true'
        className='pointer-events-none absolute right-2 bottom-0 -z-10 h-24 w-auto sm:right-4 sm:h-36'
      />
      <div className='flex min-w-0 flex-1 flex-col gap-1 py-4 pr-28 pl-4 sm:gap-1.5 sm:py-6 sm:pr-44 sm:pl-6 md:max-w-[58%] md:pr-0'>
        <p className='text-primary truncate text-[0.6875rem] font-semibold tracking-[0.2em] uppercase'>
          {t(mascot.eyebrowKey)}
        </p>
        <p className='truncate text-lg font-semibold sm:text-2xl'>
          {t('Welcome back!')} <span>{display.displayName}</span>
        </p>
        <p className='text-muted-foreground line-clamp-2 text-sm'>
          {t(mascot.taglineKey)}
        </p>
        <a
          href={MASCOT_ART_SOURCE_URL}
          target='_blank'
          rel='noreferrer'
          className='text-muted-foreground hover:text-foreground w-fit max-w-full text-xs underline-offset-2 hover:underline'
        >
          {t('Character art: {{credit}}', { credit: mascot.credit })}
        </a>
      </div>
    </section>
  )
}
