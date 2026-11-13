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
import { Radio } from '@base-ui/react/radio'
import { RadioGroup } from '@base-ui/react/radio-group'
import { CircleCheck } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { THEME_PRESETS, type ThemePreset } from '@/lib/theme-customization'
import { cn } from '@/lib/utils'

const DEFAULT_PRESET_SWATCH =
  'linear-gradient(135deg, oklch(0.68 0.2 25) 0%, oklch(0.8 0.17 85) 25%, oklch(0.72 0.18 155) 50%, oklch(0.66 0.19 245) 75%, oklch(0.68 0.2 315) 100%)'

type ThemePresetPickerProps = {
  /** The selected preset; a value outside the registry selects nothing. */
  value: string
  onValueChange: (preset: ThemePreset) => void
  ariaLabel: string
  className?: string
}

/** Swatch grid for choosing one of the registered color presets. */
export function ThemePresetPicker(props: ThemePresetPickerProps) {
  const { t } = useTranslation()
  return (
    <RadioGroup
      value={props.value}
      onValueChange={(value) => props.onValueChange(value as ThemePreset)}
      className={cn('grid w-full grid-cols-4 gap-3', props.className)}
      aria-label={props.ariaLabel}
    >
      {THEME_PRESETS.map((preset) => (
        <Radio.Root
          key={preset.value}
          value={preset.value}
          className='group flex flex-col items-stretch outline-none'
          aria-label={t(`preset.${preset.value}`)}
        >
          <div
            className={cn(
              'ring-border relative h-12 rounded-md ring-[1px] transition',
              'group-data-checked:ring-primary group-data-checked:shadow-md',
              'group-focus-visible:ring-2',
              'group-hover:ring-primary/60'
            )}
          >
            <div
              aria-hidden='true'
              className='absolute inset-0 rounded-md'
              style={{
                background:
                  preset.value === 'default'
                    ? DEFAULT_PRESET_SWATCH
                    : `linear-gradient(135deg, ${preset.swatches[0]} 0%, ${preset.swatches[1]} 100%)`,
              }}
            />
            <CircleCheck
              className={cn(
                'fill-primary absolute top-0 right-0 z-10 size-5 translate-x-1/2 -translate-y-1/2 stroke-white',
                'group-data-unchecked:hidden'
              )}
              aria-hidden='true'
            />
          </div>
          <div className='mt-1.5 truncate text-center text-xs'>
            {t(`preset.${preset.value}`)}
          </div>
        </Radio.Root>
      ))}
    </RadioGroup>
  )
}
