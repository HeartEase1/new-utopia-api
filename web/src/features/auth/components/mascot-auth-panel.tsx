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

import { MASCOT_ART_SOURCE_URL, type MascotTheme } from '@/lib/mascot-themes'

type MascotAuthPanelProps = {
  mascot: MascotTheme
}

/**
 * Left half of the auth pages under a character theme (desktop only): the
 * character's scenic illustration framed like a postcard, with her greeting
 * underneath. Purely decorative; the form column is untouched.
 */
export function MascotAuthPanel(props: MascotAuthPanelProps) {
  const { t } = useTranslation()

  return (
    <aside
      aria-label={t(props.mascot.eyebrowKey)}
      className='relative hidden flex-col items-center justify-center gap-6 overflow-hidden border-r bg-[linear-gradient(160deg,color-mix(in_oklch,var(--chart-2)_18%,var(--background))_0%,var(--background)_50%,color-mix(in_oklch,var(--primary)_16%,var(--background))_100%)] px-10 pt-20 pb-10 lg:flex'
    >
      <img
        src={props.mascot.portrait}
        alt=''
        aria-hidden='true'
        className='bg-card aspect-[2/3] max-h-[58svh] w-auto max-w-full -rotate-2 rounded-3xl border-4 border-[var(--card)] object-cover shadow-xl'
      />
      <div className='flex max-w-sm flex-col items-center gap-2 text-center'>
        <p className='text-primary text-xs font-semibold tracking-[0.2em] uppercase'>
          {t(props.mascot.eyebrowKey)}
        </p>
        <p className='text-lg leading-snug font-medium text-balance'>
          {t(props.mascot.taglineKey)}
        </p>
        <a
          href={MASCOT_ART_SOURCE_URL}
          target='_blank'
          rel='noreferrer'
          className='text-muted-foreground hover:text-foreground text-xs underline-offset-2 hover:underline'
        >
          {t('Character art: {{credit}}', { credit: props.mascot.credit })}
        </a>
      </div>
    </aside>
  )
}
