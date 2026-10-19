import { useState } from 'react'
import { ChevronDown, ListChecks, Pencil, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { CollapsibleTrigger } from '@/components/ui/collapsible'
import { Field, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { Textarea } from '@/components/ui/textarea'
import type { LoreIndexGroup, LoreItem } from '@/lib/api-client/types'
import { LoreIndexItems, type LoreIndexItemAction } from './LoreIndexItems'
import { LoreMarkdownEditor } from './LoreMarkdownEditor'
import { LoreIndexDetailToggle } from './LoreIndexMemberships'
import { LoreIndexLinkItems } from './LoreIndexLinkItems'
import { LoreIndexDisclosure } from './LoreIndexDisclosure'
import { useIndexSortable } from './LoreIndexSorting'

export function LoreIndexSection({ projectId, group, groups, items, members, mode, busy, open, onOpenChange, onChange, onReorder, onRemove, onSelect, onUpdateItems, onLinkItems, onSave }: {
  projectId: string; group: LoreIndexGroup; items: LoreItem[]; members: LoreItem[]; mode: 'rich' | 'source'; busy: boolean
  groups: LoreIndexGroup[]
  open: boolean; onOpenChange: (open: boolean) => void
  onChange: (patch: Partial<LoreIndexGroup>) => void; onReorder: (ids: string[]) => void; onRemove: () => void
  onSelect: (id: string) => void; onUpdateItems: (items: LoreItem[], action: LoreIndexItemAction) => Promise<string[]>
  onLinkItems: (items: LoreItem[]) => Promise<string[]>
  onSave: () => void
}) {
  const { t } = useTranslation()
  const [settings, setSettings] = useState(false)
  const [selecting, setSelecting] = useState(false)
  const { containerProps, activatorProps } = useIndexSortable(`custom:${group.id}`, busy)
  const enabledCount = members.filter(item => item.enabled).length
  const available = items.filter(item => !members.some(member => member.id === item.id))
  return <LoreIndexDisclosure {...containerProps} open={open} onOpenChange={expanded => { if (!expanded) setSelecting(false); onOpenChange(expanded) }} className="@container/lore-group min-w-0 rounded-lg border" data-testid="lore-index-section" header={
    <div className="flex min-w-0 items-center gap-1 p-1">
      <CollapsibleTrigger asChild>
        <Button {...activatorProps} variant="ghost" className="min-w-0 flex-1 cursor-grab justify-start gap-2 px-2 text-left active:cursor-grabbing data-[state=closed]:[&>svg]:-rotate-90" aria-label={t('lore.index.toggleSection', { name: group.name })} title={`${group.name} · ${t('lore.index.memberCount', { count: enabledCount })} · ${t(`lore.index.detail.${group.default_detail}`)}`}>
          <ChevronDown className="size-4 shrink-0 transition-transform" />
          <span className="min-w-0 flex-1 truncate">{group.name}</span>
          <Badge variant="secondary" title={t('lore.index.memberCount', { count: enabledCount })}>{enabledCount}</Badge>
          <span className="hidden shrink-0 text-xs font-normal text-muted-foreground @sm/lore-group:inline">{t(`lore.index.detail.${group.default_detail}`)}</span>
        </Button>
      </CollapsibleTrigger>
      <div className="ml-auto flex shrink-0 items-center gap-1">
      <Popover open={settings} onOpenChange={setSettings}>
        <PopoverTrigger asChild><Button variant="ghost" size="icon-sm" aria-label={t('lore.index.editSection', { name: group.name })} title={t('lore.index.editSection', { name: group.name })}><Pencil /></Button></PopoverTrigger>
        <PopoverContent align="end" className="w-80 max-w-[calc(100vw-2rem)]">
          <FieldGroup className="gap-4">
            <Field><FieldLabel htmlFor={`title-${group.id}`}>{t('lore.index.sectionName')}</FieldLabel><Input id={`title-${group.id}`} value={group.name} onChange={e => onChange({ name: e.target.value })} /></Field>
            <Field><FieldLabel htmlFor={`purpose-${group.id}`}>{t('lore.index.purpose')}</FieldLabel><Textarea id={`purpose-${group.id}`} value={group.purpose} onChange={e => onChange({ purpose: e.target.value })} placeholder={t('lore.index.purposeHint')} /></Field>
            <Button variant="secondary" onClick={() => setSettings(false)}>{t('lore.index.done')}</Button>
          </FieldGroup>
        </PopoverContent>
      </Popover>
      <Button variant="ghost" size="icon-sm" aria-label={t('lore.index.removeSection', { name: group.name })} title={t('lore.index.removeSection', { name: group.name })} onClick={onRemove}><Trash2 /></Button>
      <Button variant="ghost" size="icon-sm" disabled={busy || !members.length} aria-label={t('lore.index.multiSelect')} title={t('lore.index.multiSelect')} aria-pressed={selecting} onClick={() => { setSelecting(!selecting); onOpenChange(true) }}><ListChecks /></Button>
      </div>
    </div>
  }>
      <div className="flex min-w-0 flex-col gap-3 border-t p-3">
        {group.purpose && <p className="break-words text-xs leading-relaxed text-muted-foreground">{group.purpose}</p>}
        <div className="flex flex-wrap items-center justify-between gap-2">
          <div className="flex flex-wrap items-center gap-2">
            <span className="text-xs text-muted-foreground">{t('lore.index.defaultDetail')}</span>
            <LoreIndexDetailToggle value={group.default_detail} label={t('lore.index.defaultDetail')} onChange={detail => { if (detail !== 'inherit') onChange({ default_detail: detail }) }} />
          </div>
          <LoreIndexLinkItems items={available} busy={busy} onLink={onLinkItems} />
        </div>
        <LoreIndexItems key={selecting ? 'selecting' : 'browse'} projectId={projectId} items={members} groups={groups} group={group} busy={busy} selecting={selecting} onDone={() => setSelecting(false)} onReorder={onReorder} onSelect={onSelect} onUpdate={onUpdateItems} />
        {!members.length && <p className="text-xs text-muted-foreground">{t('lore.index.noMembers')}</p>}
        <LoreIndexDisclosure defaultOpen={Boolean(group.body_markdown.trim())} className="min-w-0" contentClassName="pt-2" header={
          <CollapsibleTrigger asChild><Button variant="ghost" size="sm" className="h-auto min-h-8 w-full justify-between py-1 data-[state=closed]:[&>svg]:-rotate-90">{t('lore.index.body')}<ChevronDown className="transition-transform" /></Button></CollapsibleTrigger>
        }>
            <LoreMarkdownEditor projectId={projectId} items={items} onOpenReference={name => { const item = items.find(i => i.name === name); if (item) onSelect(item.id) }} value={group.body_markdown} onChange={body_markdown => onChange({ body_markdown })} mode={mode} aria-label={t('lore.index.bodyFor', { name: group.name })} className="rounded-md border px-3 py-2 [&_.tiptap]:min-h-12!" onSaveShortcut={onSave} />
        </LoreIndexDisclosure>
      </div>
  </LoreIndexDisclosure>
}
