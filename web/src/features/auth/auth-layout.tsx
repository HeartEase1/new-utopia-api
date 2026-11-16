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
import { Link } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'

import { Skeleton } from '@/components/ui/skeleton'
import { useThemeCustomization } from '@/context/theme-customization-provider'
import { useSystemConfig } from '@/hooks/use-system-config'
import { MASCOT_ART_SOURCE_URL, MASCOT_THEMES } from '@/lib/mascot-themes'
import { cn } from '@/lib/utils'

import { MascotAuthPanel } from './components/mascot-auth-panel'

type AuthLayoutProps = {
  children: React.ReactNode
}

export function AuthLayout({ children }: AuthLayoutProps) {
  const { t } = useTranslation()
  const { systemName, logo, loading } = useSystemConfig()
  const { customization } = useThemeCustomization()
  // Character themes add a decorative panel and a peeking figure around the
  // form; the form column itself is the same under every preset.
  const mascot = MASCOT_THEMES[customization.preset]

  return (
    <div
      className={cn(
        'relative grid min-h-svh max-w-none',
        mascot &&
          'overflow-x-clip max-lg:bg-[linear-gradient(170deg,color-mix(in_oklch,var(--chart-2)_16%,var(--background))_0%,var(--background)_45%,color-mix(in_oklch,var(--primary)_12%,var(--background))_100%)] lg:grid-cols-2'
      )}
    >
      <Link
        to='/'
        className='absolute top-4 left-4 z-10 flex items-center gap-2 transition-opacity hover:opacity-80 sm:top-8 sm:left-8'
      >
        <div className='relative h-8 w-8'>
          {loading ? (
            <Skeleton className='absolute inset-0 rounded-full' />
          ) : (
            <img
              src={logo}
              alt={t('Logo')}
              className='h-8 w-8 rounded-full object-cover'
            />
          )}
        </div>
        {loading ? (
          <Skeleton className='h-6 w-24' />
        ) : (
          <h1 className='text-xl font-medium'>{systemName}</h1>
        )}
      </Link>
      {mascot && <MascotAuthPanel mascot={mascot} />}
      {mascot && (
        <img
          src={mascot.peek}
          alt=''
          aria-hidden='true'
          className='pointer-events-none absolute top-2 right-0 h-20 w-auto sm:top-auto sm:bottom-0 sm:h-[min(10rem,24svh)] lg:h-[min(14rem,28svh)]'
        />
      )}
      {mascot && (
        <a
          href={MASCOT_ART_SOURCE_URL}
          target='_blank'
          rel='noreferrer'
          className='text-muted-foreground hover:text-foreground absolute bottom-2 left-4 z-10 text-[0.625rem] underline-offset-2 hover:underline lg:hidden'
        >
          {t('Character art: {{credit}}', { credit: mascot.credit })}
        </a>
      )}
      <div className='relative container flex items-center pt-16 sm:pt-0'>
        <div className='mx-auto flex w-full flex-col justify-center space-y-2 px-4 py-8 sm:w-[480px] sm:p-8'>
          {children}
        </div>
      </div>
    </div>
  )
}
