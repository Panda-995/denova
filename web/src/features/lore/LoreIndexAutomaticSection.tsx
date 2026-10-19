import { useState } from 'react'
import { ChevronDown, ListChecks, Wand2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { CollapsibleTrigger } from '@/components/ui/collapsible'
import type { LoreIndexDetail, LoreIndexGroup, LoreItem } from '@/lib/api-client/types'
import { LoreIndexItems, type LoreIndexItemAction } from './LoreIndexItems'
import { LoreIndexDisclosure } from './LoreIndexDisclosure'
import { LoreIndexDetailToggle } from './LoreIndexMemberships'
import { useIndexSortable } from './LoreIndexSorting'

export function LoreIndexAutomaticSection({ groupKey, projectId, title, items, groups, detail, busy, open, onOpenChange, onDetail, onReorder, onSelect, onUpdateItems }: {
  groupKey: string; projectId: string; title: string; items: LoreItem[]; detail: LoreIndexDetail; busy: boolean
  groups: LoreIndexGroup[]
  open: boolean; onOpenChange: (open: boolean) => void
  onDetail: (detail: LoreIndexDetail) => void; onReorder: (ids: string[]) => void; onSelect: (id: string) => void
  onUpdateItems: (items: LoreItem[], action: LoreIndexItemAction) => Promise<string[]>
}) {
  const { t } = useTranslation()
  const [selecting, setSelecting] = useState(false)
  const { containerProps, activatorProps } = useIndexSortable(groupKey, busy)
  return <LoreIndexDisclosure {...containerProps} open={open} onOpenChange={expanded => { if (!expanded) setSelecting(false); onOpenChange(expanded) }} className="@container/lore-group min-w-0 rounded-lg border" data-testid="lore-index-auto-group" header={
    <div className="flex min-w-0 flex-wrap items-center gap-2 p-2">
      <CollapsibleTrigger asChild><Button {...activatorProps} variant="ghost" className="h-auto min-h-8 min-w-24 flex-1 cursor-grab justify-start gap-2 whitespace-normal py-1 text-left active:cursor-grabbing data-[state=closed]:[&>svg:first-child]:-rotate-90">
        <ChevronDown className="transition-transform" /><span className="min-w-0 flex-1 break-words">{title}</span>
        <Wand2 aria-label={t('lore.index.automatic')} /><Badge variant="secondary">{items.length}</Badge>
      </Button></CollapsibleTrigger>
      <div className="order-2 w-full @sm/lore-group:order-none @sm/lore-group:w-auto"><LoreIndexDetailToggle value={detail} label={t('lore.index.itemDetail', { name: title })} onChange={value => { if (value !== 'inherit') onDetail(value) }} /></div>
      <Button variant="ghost" size="icon-sm" disabled={busy || !items.length} aria-label={t('lore.index.multiSelect')} title={t('lore.index.multiSelect')} aria-pressed={selecting} onClick={() => { setSelecting(!selecting); onOpenChange(true) }}><ListChecks /></Button>
    </div>
  }>
    <div className="min-w-0 border-t p-3"><LoreIndexItems key={selecting ? 'selecting' : 'browse'} projectId={projectId} items={items} groups={groups} busy={busy} selecting={selecting} onDone={() => setSelecting(false)} onReorder={onReorder} onSelect={onSelect} onUpdate={onUpdateItems} /></div>
  </LoreIndexDisclosure>
}
