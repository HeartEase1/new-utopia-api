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
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Reorder } from 'motion/react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { AutoGroupOrderItem } from '@/components/auto-group-order-item'
import { Dialog } from '@/components/dialog'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { getSystemOptions } from '@/features/system-settings/api'
import { useUpdateOption } from '@/features/system-settings/hooks/use-update-option'
import { getGroups } from '@/features/users/api'
import { requireServerSuccess } from '@/lib/server-error-message'

import {
  completePricingGroupLayout,
  movePricingGroup,
  parsePricingGroupLayout,
  PRICING_GROUP_LAYOUT_KEY,
  type PricingGroupLayoutItem,
} from '../lib/group-layout'

export default function GroupLayoutEditor(props: { onClose: () => void }) {
  const { t } = useTranslation()
  const query = useQuery({
    queryKey: ['pricing-group-layout-editor'],
    queryFn: async () => {
      const [options, groups] = await Promise.all([
        getSystemOptions(),
        getGroups(),
      ])
      requireServerSuccess(options)
      requireServerSuccess(groups)
      const saved = parsePricingGroupLayout(
        options.data.find((o) => o.key === PRICING_GROUP_LAYOUT_KEY)?.value ??
          ''
      )
      return completePricingGroupLayout(
        saved,
        [
          ...new Set([
            ...(groups.data ?? []),
            ...saved.flatMap((item) => item.groups),
          ]),
        ],
        true
      )
    },
    staleTime: 0,
    gcTime: 0,
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  })
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) props.onClose()
      }}
      title={t('Arrange groups')}
      description={t(
        'Reorder groups and collect related groups into named drawers. Changes apply to all visitors.'
      )}
      contentClassName='sm:max-w-4xl'
    >
      {query.isPending && <LoadingState />}
      {query.isError && <ErrorState onRetry={() => void query.refetch()} />}
      {query.data && !query.isFetching && (
        <GroupLayoutForm initialLayout={query.data} onClose={props.onClose} />
      )}
    </Dialog>
  )
}

function GroupLayoutForm(props: {
  initialLayout: PricingGroupLayoutItem[]
  onClose: () => void
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const update = useUpdateOption()
  const [layout, setLayout] = useState(props.initialLayout)
  const [drawerIds, setDrawerIds] = useState(
    () =>
      new Set(
        props.initialLayout
          .filter((item) => item.title !== '')
          .map((item) => item.id)
      )
  )
  const drawers = layout.filter((item) => drawerIds.has(item.id))
  const valid =
    layout.length <= 1000 &&
    drawers.every(
      (item) => item.title.trim().length > 0 && [...item.title].length <= 80
    )

  const save = async () => {
    try {
      await update.mutateAsync({
        key: PRICING_GROUP_LAYOUT_KEY,
        value: JSON.stringify(layout),
      })
      await queryClient.invalidateQueries({ queryKey: ['pricing'] })
      await queryClient.invalidateQueries({
        queryKey: ['pricing-group-layout-editor'],
      })
      props.onClose()
    } catch {
      // The shared settings mutation reports failures and keeps the draft open.
    }
  }

  const moveEntry = (index: number, direction: 'up' | 'down') => {
    const target = index + (direction === 'up' ? -1 : 1)
    if (target < 0 || target >= layout.length) return
    const next = [...layout]
    ;[next[index], next[target]] = [next[target], next[index]]
    setLayout(next)
  }

  const changeItem = (id: string, patch: Partial<PricingGroupLayoutItem>) => {
    setLayout((current) =>
      current.map((item) => (item.id === id ? { ...item, ...patch } : item))
    )
  }

  return (
    <fieldset disabled={update.isPending} className='min-w-0 space-y-4'>
      <div className='flex flex-wrap items-center gap-2'>
        <Button
          type='button'
          variant='outline'
          onClick={() => {
            const id = `drawer:${crypto.randomUUID()}`
            setDrawerIds((current) => new Set([...current, id]))
            setLayout((current) => [
              ...current,
              {
                id,
                title: t('New drawer'),
                groups: [],
                default_open: false,
              },
            ])
          }}
        >
          {t('Add drawer')}
        </Button>
        <p className='text-muted-foreground text-xs'>
          {t(
            'Drag to reorder, or use the arrow buttons. New groups appear at the end.'
          )}
        </p>
      </div>
      <Reorder.Group
        axis='y'
        values={layout.map((item) => item.id)}
        onReorder={(ids) =>
          setLayout((current) => {
            const byId = new Map(current.map((item) => [item.id, item]))
            return ids.flatMap((id) => {
              const item = byId.get(id)
              return item ? [item] : []
            })
          })
        }
        className='space-y-3'
      >
        {layout.map((item, index) => (
          <AutoGroupOrderItem
            key={item.id}
            value={item.id}
            group={item.title || item.groups[0]}
            index={index}
            count={layout.length}
            onMove={moveEntry}
            onRemove={
              drawerIds.has(item.id)
                ? () =>
                    setLayout((current) =>
                      current.flatMap((entry) =>
                        entry.id === item.id
                          ? entry.groups.map((group) => ({
                              id: `group:${group}`,
                              title: '',
                              groups: [group],
                              default_open: true,
                            }))
                          : [entry]
                      )
                    )
                : undefined
            }
          >
            {drawerIds.has(item.id) ? (
              <div className='w-full min-w-0 space-y-3'>
                <Input
                  aria-label={t('Drawer name')}
                  maxLength={80}
                  value={item.title}
                  onChange={(event) =>
                    changeItem(item.id, { title: event.target.value })
                  }
                />
                <label className='flex items-center gap-2 text-xs'>
                  <Checkbox
                    checked={item.default_open}
                    onCheckedChange={(checked) =>
                      changeItem(item.id, { default_open: checked })
                    }
                  />
                  {t('Expand by default')}
                </label>
                <Reorder.Group
                  axis='y'
                  values={item.groups}
                  onReorder={(groups) => changeItem(item.id, { groups })}
                  className='space-y-2'
                >
                  {item.groups.map((group, groupIndex) => (
                    <AutoGroupOrderItem
                      key={group}
                      group={group}
                      index={groupIndex}
                      count={item.groups.length}
                      onMove={(i, direction) => {
                        const groups = [...item.groups]
                        const target = i + (direction === 'up' ? -1 : 1)
                        if (target < 0 || target >= groups.length) return
                        ;[groups[i], groups[target]] = [
                          groups[target],
                          groups[i],
                        ]
                        changeItem(item.id, { groups })
                      }}
                    >
                      <GroupDestination
                        group={group}
                        value={item.id}
                        drawers={drawers}
                        onChange={(target) =>
                          setLayout((current) =>
                            movePricingGroup(current, group, target, drawerIds)
                          )
                        }
                      />
                    </AutoGroupOrderItem>
                  ))}
                </Reorder.Group>
                {item.groups.length === 0 && (
                  <p className='text-muted-foreground text-xs'>
                    {t('Choose this drawer from a group to move it here.')}
                  </p>
                )}
              </div>
            ) : (
              <GroupDestination
                group={item.groups[0]}
                value=''
                drawers={drawers}
                onChange={(target) =>
                  setLayout((current) =>
                    movePricingGroup(current, item.groups[0], target, drawerIds)
                  )
                }
              />
            )}
          </AutoGroupOrderItem>
        ))}
      </Reorder.Group>
      <div className='flex justify-end gap-2'>
        <Button type='button' variant='outline' onClick={props.onClose}>
          {t('Cancel')}
        </Button>
        <Button
          type='button'
          disabled={!valid || update.isPending}
          onClick={() => void save()}
        >
          {update.isPending ? t('Saving...') : t('Save')}
        </Button>
      </div>
    </fieldset>
  )
}

function GroupDestination(props: {
  group: string
  value: string
  drawers: PricingGroupLayoutItem[]
  onChange: (id: string) => void
}) {
  const { t } = useTranslation()
  return (
    <NativeSelect
      aria-label={t('Drawer for {{group}}', { group: props.group })}
      value={props.value}
      onChange={(event) => props.onChange(event.target.value)}
      className='max-w-full'
    >
      <NativeSelectOption value=''>{t('Standalone group')}</NativeSelectOption>
      {props.drawers.map((drawer) => (
        <NativeSelectOption key={drawer.id} value={drawer.id}>
          {drawer.title}
        </NativeSelectOption>
      ))}
    </NativeSelect>
  )
}
