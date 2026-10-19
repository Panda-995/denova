import { useId, useState } from 'react'
import { MoreHorizontal } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Checkbox } from '@/components/ui/checkbox'
import { Field, FieldContent, FieldDescription, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import type { LoreIndexGroup, LoreIndexMembership, LoreItem } from '@/lib/api-client/types'
import { cn } from '@/lib/utils'
import { LoreItemCard, LORE_CARD_LAYOUTS } from './LoreCard'
import { LoreIndexSortArea, LoreIndexSortableCard } from './LoreIndexSorting'
import { LOAD_MODE_OPTIONS } from './options'

/** Item settings use the canonical Lore record. Membership edits affect only
 * the named group; load mode remains the fallback for automatic grouping. */
export type LoreIndexItemAction =
  | { kind: 'load-mode'; value: LoreItem['load_mode'] }
  | { kind: 'enabled'; value: boolean }
  | { kind: 'link'; groupID: string }
  | { kind: 'membership'; groupID: string; detail: LoreIndexMembership['detail'] | 'remove' }

type UpdateItems = (items: LoreItem[], action: LoreIndexItemAction) => Promise<string[]>

function LoadingOptions({ items, groups, group, busy, label, trigger, onUpdate }: {
  items: LoreItem[]; groups: LoreIndexGroup[]; group?: LoreIndexGroup; busy: boolean
  label: string; trigger: 'card' | 'batch'; onUpdate: UpdateItems
}) {
  const { t } = useTranslation()
  const enabledID = useId()
  const [open, setOpen] = useState(false)
  const single = items.length === 1 ? items[0] : undefined
  const enabled = items.every(item => item.enabled)
  const mixedEnabled = !enabled && items.some(item => item.enabled)
  const loadMode = items.every(item => item.load_mode === items[0]?.load_mode) ? items[0]?.load_mode ?? '' : ''
  const detail = single?.index_memberships?.find(m => m.group_id === group?.id)?.detail
  const update = async (action: LoreIndexItemAction) => {
    const saved = await onUpdate(items, action)
    if (saved.length === items.length) setOpen(false)
  }
  return <Popover open={open} onOpenChange={value => { if (!busy) setOpen(value) }}>
    <PopoverTrigger asChild>
      <Button variant="ghost" size={trigger === 'card' ? 'icon-sm' : 'sm'} disabled={busy || !items.length} aria-label={label} title={label}
        className={cn(trigger === 'card' && 'bg-background/90 shadow-sm transition-opacity [@media(hover:hover)]:opacity-0 group-hover/card:opacity-100 group-focus-within/card:opacity-100 data-[state=open]:opacity-100')}>
        <MoreHorizontal />{trigger === 'batch' && label}
      </Button>
    </PopoverTrigger>
    <PopoverContent data-index-control align="end" className="w-80 max-w-[calc(100vw-2rem)] max-h-[var(--radix-popover-content-available-height)] overflow-y-auto" aria-label={label} onClick={event => event.stopPropagation()}>
      <FieldGroup className="gap-3">
        <Field orientation="horizontal" data-disabled={busy}>
          <FieldContent className="min-w-0">
            <FieldLabel htmlFor={enabledID}>{t('settingPanel.field.enabled')}</FieldLabel>
            <FieldDescription>{t(mixedEnabled ? 'lore.index.mixedEnabled' : enabled ? 'settingPanel.enabled' : 'settingPanel.disabled')}</FieldDescription>
          </FieldContent>
          {/* A mixed selection needs a direct disable action as well as the
              switch's enable action, without changing any entries first. */}
          {mixedEnabled && <Button size="sm" variant="ghost" disabled={busy} onClick={() => void update({ kind: 'enabled', value: false })}>{t('settingPanel.disabled')}</Button>}
          <Switch id={enabledID} checked={enabled} disabled={busy} aria-label={t('settingPanel.field.enabled')} onCheckedChange={value => void update({ kind: 'enabled', value })} />
        </Field>
        <Field>
          <FieldLabel>{t('settingPanel.field.loadMode')}</FieldLabel>
          <ToggleGroup type="single" variant="outline" spacing={1} value={loadMode} disabled={busy} aria-label={t('settingPanel.field.loadMode')} className="w-full flex-wrap"
            onValueChange={value => { if (value) void update({ kind: 'load-mode', value: value as LoreItem['load_mode'] }) }}>
            {LOAD_MODE_OPTIONS.map(({ value }) => <ToggleGroupItem key={value} value={value} className="min-w-0 flex-1 px-2">{t(`lore.index.loadMode.${value}`)}</ToggleGroupItem>)}
          </ToggleGroup>
          <FieldDescription>{t('lore.index.loadModeHint')}</FieldDescription>
        </Field>
        {group && <Field>
          <FieldLabel>{t('lore.index.entryDetail')}</FieldLabel>
          <Select value={detail ?? ''} disabled={busy} onValueChange={value => void update({ kind: 'membership', groupID: group.id, detail: value as LoreIndexMembership['detail'] })}>
            <SelectTrigger className="w-full" aria-label={t('lore.index.entryDetail')}><SelectValue placeholder={t('lore.index.chooseValue')} /></SelectTrigger>
            <SelectContent data-index-control><SelectGroup>
              <SelectItem value="inherit">{t('lore.index.inherit', { detail: t(`lore.index.detail.${group.default_detail}`) })}</SelectItem>
              {(['name', 'brief', 'full'] as const).map(value => <SelectItem key={value} value={value}>{t(`lore.index.detail.${value}`)}</SelectItem>)}
            </SelectGroup></SelectContent>
          </Select>
        </Field>}
        {groups.some(candidate => candidate.id !== group?.id) && <Field>
          <FieldLabel>{t('lore.index.addToGroup')}</FieldLabel>
          <Select value="" disabled={busy} onValueChange={groupID => void update({ kind: 'link', groupID })}>
            <SelectTrigger className="w-full" aria-label={t('lore.index.addToGroup')}><SelectValue placeholder={t('lore.index.chooseGroup')} /></SelectTrigger>
            <SelectContent data-index-control><SelectGroup>{groups.filter(candidate => candidate.id !== group?.id).map(candidate => <SelectItem key={candidate.id} value={candidate.id} className="max-w-72 break-all whitespace-normal">{candidate.name}</SelectItem>)}</SelectGroup></SelectContent>
          </Select>
        </Field>}
        {group && <Button variant="outline" disabled={busy} onClick={() => void update({ kind: 'membership', groupID: group.id, detail: 'remove' })}>{t('lore.index.removeFromGroup')}</Button>}
      </FieldGroup>
    </PopoverContent>
  </Popover>
}

/** Group headers own selection mode and remount this component when it changes.
 * Partial saves clear successful IDs so retries only submit failed entries. */
export function LoreIndexItems({ projectId, items, groups, group, busy, selecting, onDone, onReorder, onSelect, onUpdate }: {
  projectId: string; items: LoreItem[]; groups: LoreIndexGroup[]; group?: LoreIndexGroup; busy: boolean
  selecting: boolean; onDone: () => void
  onReorder: (ids: string[]) => void; onSelect: (id: string) => void; onUpdate: UpdateItems
}) {
  const { t } = useTranslation()
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const selectedItems = items.filter(item => selected.has(item.id))
  const toggle = (id: string) => {
    if (busy) return
    setSelected(current => {
      const next = new Set(current)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }
  const updateSelected: UpdateItems = async (entries, action) => {
    const saved = await onUpdate(entries, action)
    setSelected(current => new Set([...current].filter(id => !saved.includes(id))))
    return saved
  }
  return <div className="flex min-w-0 flex-col gap-2" data-testid="lore-index-items">
    {selecting && <div className="flex min-w-0 flex-wrap items-center gap-1">
      <span className="mr-auto text-xs text-muted-foreground" aria-live="polite">{t('lore.index.selectedCount', { count: selectedItems.length })}</span>
      <Button size="sm" variant="ghost" disabled={busy || !items.length} onClick={() => setSelected(new Set(selectedItems.length === items.length ? [] : items.map(item => item.id)))}>{t(selectedItems.length === items.length ? 'lore.index.clearSelection' : 'lore.index.selectAll')}</Button>
      <LoadingOptions items={selectedItems} groups={groups} group={group} busy={busy} trigger="batch" label={t('lore.index.batchActions')} onUpdate={updateSelected} />
      <Button size="sm" variant="ghost" disabled={busy} onClick={onDone}>{t('lore.index.done')}</Button>
    </div>}
    <LoreIndexSortArea ids={items.map(item => item.id)} layout="grid" onReorder={onReorder}>
      <div className={cn('grid min-w-0', LORE_CARD_LAYOUTS.small)}>{items.map(item => <LoreIndexSortableCard key={item.id} id={item.id} name={item.name} disabled={busy || selecting}>
        <LoreItemCard projectId={projectId} item={item} cardSize="small" selected={selecting && selected.has(item.id)}
          onSelect={() => { if (!busy) { if (selecting) toggle(item.id); else onSelect(item.id) } }}
          coverAction={selecting
            ? <div className="flex size-8 items-center justify-center rounded-md bg-background/90 shadow-sm"><Checkbox checked={selected.has(item.id)} disabled={busy} onCheckedChange={() => toggle(item.id)} aria-label={t('lore.library.selectItem', { name: item.name })} /></div>
            : <>{!item.enabled && <Badge variant="outline" className="bg-background/90">{t('lore.references.disabled')}</Badge>}<LoadingOptions items={[item]} groups={groups} group={group} busy={busy} trigger="card" label={t('lore.index.loadingOptionsFor', { name: item.name })} onUpdate={onUpdate} /></>}
        />
      </LoreIndexSortableCard>)}</div>
    </LoreIndexSortArea>
  </div>
}
