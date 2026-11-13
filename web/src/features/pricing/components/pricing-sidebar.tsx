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
import { ChevronDown, RotateCcw } from 'lucide-react'
import { memo, useEffect, useMemo, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import { formatNumber } from '@/lib/format'
import { getLobeIcon } from '@/lib/lobe-icon'
import { cn } from '@/lib/utils'

import {
  ENDPOINT_TYPES,
  FILTER_ALL,
  QUOTA_TYPES,
  getEndpointTypeLabels,
  getQuotaTypeLabels,
} from '../constants'
import { getModelQuotaType, parseTags } from '../lib/filters'
import {
  completePricingGroupLayout,
  type PricingGroupLayoutItem,
} from '../lib/group-layout'
import type { PricingModel, PricingVendor } from '../types'

type FilterOption = {
  value: string
  label: string
  count?: number
  suffix?: string
  icon?: ReactNode
}

type FilterSectionProps = {
  title: string
  value: string
  options: FilterOption[]
  onChange: (value: string) => void
  children?: ReactNode
}

export interface PricingSidebarProps {
  quotaTypeFilter: string
  endpointTypeFilter: string
  vendorFilter: string
  groupFilter: string
  tagFilter: string
  onQuotaTypeChange: (value: string) => void
  onEndpointTypeChange: (value: string) => void
  onVendorChange: (value: string) => void
  onGroupChange: (value: string) => void
  onTagChange: (value: string) => void
  vendors: PricingVendor[]
  groups: string[]
  groupLayout?: PricingGroupLayoutItem[]
  groupRatios?: Record<string, number>
  tags: string[]
  models: PricingModel[]
  hasActiveFilters: boolean
  onClearFilters: () => void
  className?: string
}

function formatGroupRatio(ratio: number | undefined): string | undefined {
  if (ratio == null) return undefined
  const formatted = Number.isInteger(ratio)
    ? ratio.toString()
    : ratio.toFixed(3).replace(/0+$/, '').replace(/\.$/, '')
  return `x${formatted}`
}

function FilterChip(props: {
  option: FilterOption
  active: boolean
  onClick: () => void
}) {
  return (
    <Button
      type='button'
      variant={props.active ? 'secondary' : 'outline'}
      size='sm'
      onClick={props.onClick}
      aria-pressed={props.active}
      className='h-auto max-w-full gap-1.5 px-2 py-1 text-xs'
      title={props.option.label}
    >
      {props.option.icon && (
        <span className='shrink-0'>{props.option.icon}</span>
      )}
      <span className='truncate'>{props.option.label}</span>
      {(props.option.suffix || props.option.count != null) && (
        <span
          className={cn(
            'rounded-md px-1.5 py-0.5 text-[12px]',
            props.active
              ? 'bg-background text-foreground'
              : 'bg-muted text-muted-foreground'
          )}
        >
          {props.option.suffix ?? props.option.count}
        </span>
      )}
    </Button>
  )
}

function FilterSection(props: FilterSectionProps) {
  return (
    <Collapsible
      defaultOpen
      className='border-border/70 border-b pb-3 last:border-b-0'
    >
      <CollapsibleTrigger className='group flex w-full items-center justify-between py-2.5 text-left'>
        <span className='text-foreground text-sm font-semibold'>
          {props.title}
        </span>
        <ChevronDown className='text-muted-foreground size-4 transition-transform group-data-[panel-open]:rotate-180' />
      </CollapsibleTrigger>
      <CollapsibleContent>
        <div className='flex flex-wrap gap-1.5'>
          {props.children ??
            props.options.map((option) => (
              <FilterChip
                key={option.value}
                option={option}
                active={props.value === option.value}
                onClick={() => props.onChange(option.value)}
              />
            ))}
        </div>
      </CollapsibleContent>
    </Collapsible>
  )
}

function GroupDrawer(props: {
  item: PricingGroupLayoutItem
  selected: string
  options: Map<string, FilterOption>
  onChange: (group: string) => void
}) {
  const [open, setOpen] = useState(
    props.item.default_open || props.item.groups.includes(props.selected)
  )
  useEffect(() => {
    if (props.item.groups.includes(props.selected)) setOpen(true)
  }, [props.selected, props.item.groups])
  return (
    <Collapsible
      open={open}
      onOpenChange={setOpen}
      className='w-full min-w-0 rounded-lg border px-2'
    >
      <CollapsibleTrigger className='group flex w-full min-w-0 items-center gap-2 py-2 text-left text-xs font-medium'>
        <span className='min-w-0 flex-1 truncate' title={props.item.title}>
          {props.item.title}
        </span>{' '}
        <span className='text-muted-foreground'>
          {formatNumber(props.item.groups.length)}
        </span>
        <ChevronDown className='size-3.5 shrink-0 transition-transform group-data-[panel-open]:rotate-180' />
      </CollapsibleTrigger>
      <CollapsibleContent>
        <div className='flex flex-wrap gap-1.5 pb-2'>
          {props.item.groups.map((group) => {
            const option = props.options.get(group)
            return (
              option && (
                <FilterChip
                  key={group}
                  option={option}
                  active={props.selected === group}
                  onClick={() => props.onChange(group)}
                />
              )
            )
          })}
        </div>
      </CollapsibleContent>
    </Collapsible>
  )
}

export const PricingSidebar = memo(function PricingSidebar(
  props: PricingSidebarProps
) {
  const { t } = useTranslation()
  const counts = useMemo(() => {
    const vendors = new Map<string, number>()
    const tags = new Map<string, number>()
    const endpoints = new Map<string, number>()
    const quotas = { token: 0, request: 0, task: 0 }
    for (const model of props.models) {
      if (model.vendor_name) {
        vendors.set(
          model.vendor_name,
          (vendors.get(model.vendor_name) ?? 0) + 1
        )
      }
      for (const tag of new Set(
        parseTags(model.tags).map((tag) => tag.toLowerCase())
      )) {
        tags.set(tag, (tags.get(tag) ?? 0) + 1)
      }
      for (const endpoint of new Set(model.supported_endpoint_types ?? [])) {
        endpoints.set(endpoint, (endpoints.get(endpoint) ?? 0) + 1)
      }
      const quotaType = getModelQuotaType(model)
      if (quotaType) quotas[quotaType]++
    }
    return { vendors, tags, endpoints, quotas }
  }, [props.models])
  const quotaTypeLabels = getQuotaTypeLabels(t)
  const endpointTypeLabels = getEndpointTypeLabels(t)

  const vendorOptions: FilterOption[] = [
    {
      value: FILTER_ALL,
      label: t('All Vendors'),
      count: props.models.length,
    },
    ...props.vendors
      .map((vendor) => ({
        value: vendor.name,
        label: vendor.name,
        count: counts.vendors.get(vendor.name) ?? 0,
        icon: vendor.icon ? getLobeIcon(vendor.icon, 14) : undefined,
      }))
      .filter((vendor) => vendor.count > 0),
  ]

  const groupOptions: FilterOption[] = [
    {
      value: FILTER_ALL,
      label: t('All Groups'),
    },
    ...props.groups.map((group) => ({
      value: group,
      label: group,
      suffix: formatGroupRatio(props.groupRatios?.[group]),
    })),
  ]
  const groupLayout = useMemo(
    () => completePricingGroupLayout(props.groupLayout ?? [], props.groups),
    [props.groupLayout, props.groups]
  )
  const groupOptionMap = new Map(
    groupOptions.map((option) => [option.value, option])
  )

  const quotaOptions: FilterOption[] = [
    {
      value: QUOTA_TYPES.ALL,
      label: quotaTypeLabels[QUOTA_TYPES.ALL],
      count: props.models.length,
    },
    {
      value: QUOTA_TYPES.TOKEN,
      label: quotaTypeLabels[QUOTA_TYPES.TOKEN],
      count: counts.quotas.token,
    },
    {
      value: QUOTA_TYPES.REQUEST,
      label: quotaTypeLabels[QUOTA_TYPES.REQUEST],
      count: counts.quotas.request,
    },
    {
      value: QUOTA_TYPES.TASK,
      label: quotaTypeLabels[QUOTA_TYPES.TASK],
      count: counts.quotas.task,
    },
  ]

  const tagOptions: FilterOption[] = [
    {
      value: FILTER_ALL,
      label: t('All Tags'),
      count: props.models.length,
    },
    ...props.tags.map((tag) => ({
      value: tag,
      label: tag,
      count: counts.tags.get(tag.toLowerCase()) ?? 0,
    })),
  ]

  const endpointOptions: FilterOption[] = [
    {
      value: ENDPOINT_TYPES.ALL,
      label: endpointTypeLabels[ENDPOINT_TYPES.ALL],
      count: props.models.length,
    },
    ...Object.entries(endpointTypeLabels)
      .filter(([value]) => value !== ENDPOINT_TYPES.ALL)
      .map(([value, label]) => ({
        value,
        label,
        count: counts.endpoints.get(value) ?? 0,
      })),
  ]

  return (
    <aside className={cn('bg-card rounded-xl border p-3', props.className)}>
      <div className='mb-2.5 flex items-center justify-between gap-2'>
        <div>
          <h2 className='text-foreground text-sm font-bold'>{t('Filter')}</h2>
          <p className='text-muted-foreground mt-1 text-xs'>
            {t('Refine models by provider, group, type, and tags.')}
          </p>
        </div>
        <Button
          type='button'
          variant='ghost'
          size='sm'
          onClick={props.onClearFilters}
          disabled={!props.hasActiveFilters}
          className='h-7 gap-1.5 px-2 text-xs'
        >
          <RotateCcw className='size-3.5' />
          {t('Reset')}
        </Button>
      </div>

      {props.hasActiveFilters && (
        <Badge variant='secondary' className='mb-3'>
          {t('Filters active')}
        </Badge>
      )}

      <div className='space-y-1'>
        <FilterSection
          title={t('Groups')}
          value={props.groupFilter}
          options={groupOptions}
          onChange={props.onGroupChange}
        >
          <FilterChip
            option={groupOptions[0]}
            active={props.groupFilter === FILTER_ALL}
            onClick={() => props.onGroupChange(FILTER_ALL)}
          />
          {groupLayout.map((item) =>
            item.title ? (
              <GroupDrawer
                key={item.id}
                item={item}
                selected={props.groupFilter}
                options={groupOptionMap}
                onChange={props.onGroupChange}
              />
            ) : (
              item.groups.map((group) => (
                <FilterChip
                  key={group}
                  option={
                    groupOptionMap.get(group) ?? { value: group, label: group }
                  }
                  active={props.groupFilter === group}
                  onClick={() => props.onGroupChange(group)}
                />
              ))
            )
          )}
        </FilterSection>
        <FilterSection
          title={t('All Vendors')}
          value={props.vendorFilter}
          options={vendorOptions}
          onChange={props.onVendorChange}
        />
        <FilterSection
          title={t('Model Tags')}
          value={props.tagFilter}
          options={tagOptions}
          onChange={props.onTagChange}
        />
        <FilterSection
          title={t('Pricing Type')}
          value={props.quotaTypeFilter}
          options={quotaOptions}
          onChange={props.onQuotaTypeChange}
        />
        <FilterSection
          title={t('Endpoint Type')}
          value={props.endpointTypeFilter}
          options={endpointOptions}
          onChange={props.onEndpointTypeChange}
        />
      </div>
    </aside>
  )
})
