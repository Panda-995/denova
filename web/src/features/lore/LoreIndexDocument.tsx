import { useCallback, useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { ChevronDown, ChevronsDownUp, ChevronsUpDown, Layers, PanelRightClose, PanelRightOpen, Plus } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { CollapsibleTrigger } from '@/components/ui/collapsible'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from '@/components/ui/empty'
import { ConfirmDialog } from '@/components/common/ConfirmDialog'
import { ScrollToTopButton } from '@/components/common/ScrollToTopButton'
import { InlineErrorNotice } from '@/components/common/inline-error-notice'
import { LoadingState } from '@/components/common/LoadingState'
import { AutosaveStatusIndicator } from '@/components/forms/autosave-status'
import { CollapsiblePanelSeparator, CollapsibleResizablePanel, PanelMotionGroup } from '@/components/layout/panel-motion'
import { resolvePanelInitialSize, usePersistedPanelLayout } from '@/components/layout/use-persisted-panel-layout'
import { updateProjectLoreItem, type LoreItem } from '@/lib/api'
import type { LoreIndexGroup } from '@/lib/api-client/types'
import type { LoreIndexItemAction } from './LoreIndexItems'
import { LoreMarkdownEditor } from './LoreMarkdownEditor'
import { LoreIndexPreview, useLoreIndexPreview } from './LoreIndexPreview'
import { LoreIndexSection } from './LoreIndexSection'
import { LoreIndexAutomaticSection } from './LoreIndexAutomaticSection'
import { LoreIndexSortArea, mergeIndexOrder, orderIndexEntries } from './LoreIndexSorting'
import { LoreIndexDisclosure } from './LoreIndexDisclosure'
import { useLoreIndexDocument } from './use-lore-index-document'
import { useLoreCategories } from './use-lore-categories'
import { loreAutosaveDraft, loreAutosavePayload } from './use-lore-item-autosave'
import { notifyLoreUpdated } from './events'

export const LORE_INDEX_ID = '__lore_index__'

/** Grouped index editor with an always-running preview. The responsive
 * switch changes the visible pane only, never the guide or loading policy. */
export function LoreIndexDocument({ projectId, items, headerActionsTarget, onSelect, onChanged, onFlushHandlerChange }: {
  projectId: string
  items: LoreItem[]
  /** The workspace supplies a header slot; the document owns its save and preview controls. */
  headerActionsTarget: HTMLDivElement | null
  onSelect: (id: string) => void
  onChanged: (item: LoreItem) => void
  onFlushHandlerChange: (handler: (() => Promise<boolean>) | null) => void
}) {
  const { t } = useTranslation()
  const document = useLoreIndexDocument(projectId)
  const { sections } = useLoreCategories(projectId)
  const [mode, setMode] = useState<'rich' | 'source'>('rich')
  const [pane, setPane] = useState('document')
  const [previewVisible, setPreviewVisible] = useState(true)
  const [compact, setCompact] = useState(false)
  const [expandedGroups, setExpandedGroups] = useState<Set<string>>(new Set())
  const panelLayout = usePersistedPanelLayout({
    storageKey: 'denova:lore-index:layout',
    panelIds: ['lore-index-editor-pane', 'lore-index-preview-pane'],
  })
  const [wideLayout, setWideLayout] = useState(panelLayout.defaultLayout)
  // Measure the document itself: the library directory also consumes desktop width.
  const containerRef = useCallback((node: HTMLDivElement | null) => {
    if (!node) return
    const update = (width: number) => { if (width > 0) setCompact(width < 768) }
    update(node.clientWidth)
    let frame = 0
    const observer = new ResizeObserver(([entry]) => {
      cancelAnimationFrame(frame)
      frame = requestAnimationFrame(() => update(entry.contentRect.width))
    })
    observer.observe(node)
    return () => { observer.disconnect(); cancelAnimationFrame(frame) }
  }, [])
  const [removeGroup, setRemoveGroup] = useState<LoreIndexGroup | null>(null)
  const [busy, setBusy] = useState(false)
  const editor = useRef<HTMLDivElement>(null)
  const guide = document.guide
  const live = useLoreIndexPreview(projectId, guide, items, document.data?.revision)
  useEffect(() => {
    onFlushHandlerChange(document.flush)
    return () => onFlushHandlerChange(null)
  }, [document.flush, onFlushHandlerChange])
  const openReference = useCallback((name: string) => {
    const item = items.find(item => item.name.toLowerCase() === name.toLowerCase())
    if (item) onSelect(item.id)
  }, [items, onSelect])
  if (!guide) return document.isError
    ? <div className="flex flex-col gap-3 p-6"><InlineErrorNotice message={t('lore.index.loadFailed')} /><Button onClick={() => void document.refetch()}>{t('common.retry')}</Button></div>
    : <LoadingState label={t('common.loading')} className="h-full" />

  const changeGroup = (id: string, patch: Partial<LoreIndexGroup>) => document.setGuide({ ...guide, groups: guide.groups.map(group => group.id === id ? { ...group, ...patch } : group) })
  const reorderItems = (key: string, ids: string[]) => {
    document.setGuide({ ...guide, item_order: { ...guide.item_order, [key]: mergeIndexOrder(guide.item_order?.[key], ids) } })
    console.info('[lore-index] item order changed', { projectId, group: key, items: ids.length })
  }
  const addGroup = () => {
    let suffix = guide.groups.length + 1
    let name = t('lore.index.newSection', { number: suffix })
    while (guide.groups.some(g => g.name === name)) name = t('lore.index.newSection', { number: ++suffix })
    const id = crypto.randomUUID()
    document.setGuide({ ...guide, groups: [...guide.groups, {
      id, name, purpose: '', body_markdown: '', default_detail: 'name',
    }] })
    setExpandedGroups(current => new Set(current).add(`custom:${id}`))
    setPane('document')
    requestAnimationFrame(() => Array.from(editor.current?.querySelectorAll('[data-testid="lore-index-section"]') ?? []).at(-1)?.scrollIntoView({ block: 'nearest' }))
  }
  const updateItems = async (entries: LoreItem[], action: LoreIndexItemAction): Promise<string[]> => {
    setBusy(true)
    const savedIDs: string[] = []
    try {
      if (!(await document.flush())) return savedIDs
      for (const item of entries) {
        try {
          const payload = loreAutosavePayload(loreAutosaveDraft(item))
          switch (action.kind) {
            case 'load-mode': payload.load_mode = action.value; break
            case 'enabled': payload.enabled = action.value; break
            case 'link':
              if (item.index_memberships?.some(m => m.group_id === action.groupID)) { savedIDs.push(item.id); continue }
              payload.index_memberships = [...(item.index_memberships ?? []), { group_id: action.groupID, detail: 'inherit' }]
              break
            case 'membership': {
              const memberships = (item.index_memberships ?? []).filter(m => m.group_id !== action.groupID)
              if (action.detail !== 'remove') memberships.push({ group_id: action.groupID, detail: action.detail })
              payload.index_memberships = memberships
              break
            }
          }
          const saved = await updateProjectLoreItem(projectId, item.id, payload, item.updated_at)
          onChanged(saved)
          savedIDs.push(saved.id)
        } catch (error) {
          console.error('[lore-index] item update failed', { projectId, itemID: item.id, action, error })
          if (entries.length === 1) toast.error(t('editor.saveFailed'))
        }
      }
      if (entries.length > 1 && savedIDs.length < entries.length) toast.error(t('lore.index.updateFailedCount', { count: entries.length - savedIDs.length }))
      if (savedIDs.length) {
        console.info('[lore-index] items updated', { projectId, action, saved: savedIDs.length, total: entries.length })
        notifyLoreUpdated({ projectId, source: 'lore-index' })
      }
      return savedIDs
    } finally { setBusy(false) }
  }
  const automaticGroups = live.preview?.automatic_groups ?? []
  const groups = orderIndexEntries([
    ...guide.groups.map(group => ({ key: `custom:${group.id}`, kind: 'custom' as const, group })),
    ...automaticGroups.map(group => ({ key: `automatic:${group.key}`, kind: 'automatic' as const, group })),
  ], guide.group_order, entry => entry.key)
  const allExpanded = groups.length > 0 && groups.every(group => expandedGroups.has(group.key))
  const changeExpanded = (key: string, open: boolean) => setExpandedGroups(current => {
    const next = new Set(current)
    if (open) next.add(key)
    else next.delete(key)
    return next
  })
  const itemsByID = new Map(items.map(item => [item.id, item]))
  const groupItems = (key: string, ids: string[]) => orderIndexEntries(ids.flatMap(id => {
    const item = itemsByID.get(id)
    return item ? [item] : []
  }), guide.item_order?.[key], item => item.id)
  const previewOpen = compact ? pane === 'preview' : previewVisible
  return <div ref={containerRef} className="@container flex h-full min-h-0 min-w-0 flex-col bg-background" data-testid="lore-index-document">
    {headerActionsTarget && createPortal(<>
      <AutosaveStatusIndicator status={document.status} error={document.saveError} showSavedLabel={false} onRetry={() => void document.flush()} />
      <Button
        variant="ghost" size="icon-sm"
        aria-label={t(previewOpen ? 'lore.index.hidePreview' : 'lore.index.showPreview')}
        title={t(previewOpen ? 'lore.index.hidePreview' : 'lore.index.showPreview')}
        aria-expanded={previewOpen} aria-controls="lore-index-preview-pane"
        onClick={() => compact ? setPane(previewOpen ? 'document' : 'preview') : setPreviewVisible(visible => !visible)}
      >
        {previewOpen ? <PanelRightClose /> : <PanelRightOpen />}
      </Button>
    </>, headerActionsTarget)}
    <PanelMotionGroup
      orientation="horizontal" disabled={compact} motionSuspended={compact}
      defaultLayout={panelLayout.defaultLayout}
      onLayoutChanged={layout => {
        if (!compact && previewVisible && panelLayout.persistUserLayout(layout)) setWideLayout(layout)
      }}
      className="relative min-h-0 min-w-0 flex-1"
    >
      <CollapsibleResizablePanel
        id="lore-index-editor-pane" visible={!compact || pane === 'document'} side="left"
        minSize={compact ? (pane === 'document' ? '100%' : '0px') : '320px'} restorationKey={compact ? 'compact' : 'wide'}
        initialExpandSize={compact ? '100%' : resolvePanelInitialSize(wideLayout, 'lore-index-editor-pane', '52%')}
      >
      <div className="relative h-full min-h-0 min-w-0">
      <div ref={editor} className="h-full min-h-0 min-w-0 overflow-y-auto" data-testid="lore-index-editor">
        <article className="mx-auto flex w-full min-w-0 max-w-4xl flex-col gap-5 p-3 @3xl:p-4">
          {!document.valid && <InlineErrorNotice message={t('lore.index.invalid')} />}
          <LoreIndexDisclosure defaultOpen={Boolean(guide.intro_markdown.trim())} className="min-w-0 rounded-lg border" contentClassName="border-t p-3" header={
            <div className="flex min-w-0 flex-wrap items-center gap-2 p-2">
              <CollapsibleTrigger asChild><Button variant="ghost" size="sm" className="h-auto min-h-8 min-w-0 flex-1 justify-start py-1 data-[state=closed]:[&>svg]:-rotate-90"><ChevronDown className="transition-transform" />{t('lore.index.intro')}</Button></CollapsibleTrigger>
              <ToggleGroup type="single" value={mode} onValueChange={value => { if (value) setMode(value as typeof mode) }} aria-label={t('lore.index.editMode')}>
                <ToggleGroupItem value="rich">{t('lore.index.rich')}</ToggleGroupItem><ToggleGroupItem value="source">{t('lore.index.source')}</ToggleGroupItem>
              </ToggleGroup>
            </div>
          }>
              <p className="mb-2 text-xs leading-relaxed text-muted-foreground">{t('lore.index.introHint')}</p>
              <LoreMarkdownEditor projectId={projectId} items={items} onOpenReference={openReference} value={guide.intro_markdown} onChange={intro_markdown => document.setGuide({ ...guide, intro_markdown })} mode={mode} aria-label={t('lore.index.intro')} className="rounded-md border px-3 py-2 [&_.tiptap]:min-h-16!" onSaveShortcut={() => void document.flush()} />
          </LoreIndexDisclosure>
          <section className="flex min-w-0 flex-col gap-2">
            <div className="flex flex-wrap items-center justify-between gap-2">
              <div className="flex items-center gap-2"><Layers className="size-4 text-muted-foreground" /><h2 className="text-sm font-medium">{t('lore.index.groups')}</h2><Badge variant="secondary">{groups.length}</Badge></div>
              <div className="ml-auto flex items-center gap-1">
                {groups.length > 0 && <Button type="button" variant="outline" size="sm" onClick={() => setExpandedGroups(new Set(allExpanded ? [] : groups.map(group => group.key)))}>
                  {allExpanded ? <ChevronsDownUp data-icon="inline-start" /> : <ChevronsUpDown data-icon="inline-start" />}
                  {t(allExpanded ? 'lore.index.collapseAll' : 'lore.index.expandAll')}
                </Button>}
                <Button size="sm" aria-label={t('lore.index.addSection')} title={t('lore.index.addSection')} onClick={addGroup}><Plus data-icon="inline-start" /><span className="hidden @sm:inline">{t('lore.index.addSection')}</span></Button>
              </div>
            </div>
            <p className="text-xs leading-relaxed text-muted-foreground">{t('lore.index.sortHint')}</p>
            {!groups.length && <Empty className="gap-3 border border-dashed p-4 md:p-4"><EmptyHeader className="gap-1"><EmptyTitle>{t(items.some(item => item.enabled) ? 'lore.index.emptyTitle' : 'lore.index.noItemsTitle')}</EmptyTitle><EmptyDescription>{t('lore.index.emptyHint')}</EmptyDescription></EmptyHeader><Button variant="outline" size="sm" onClick={addGroup}><Plus data-icon="inline-start" />{t('lore.index.addFirst')}</Button></Empty>}
            <LoreIndexSortArea ids={groups.map(group => group.key)} layout="list" onReorder={ids => {
              document.setGuide({ ...guide, group_order: mergeIndexOrder(guide.group_order, ids) })
              console.info('[lore-index] group order changed', { projectId, groups: ids.length })
            }}>
              {groups.map(entry => {
                const shared = { projectId, busy, groups: guide.groups, onUpdateItems: updateItems, open: expandedGroups.has(entry.key), onOpenChange: (open: boolean) => changeExpanded(entry.key, open), onReorder: (ids: string[]) => reorderItems(entry.key, ids), onSelect }
                if (entry.kind === 'custom') {
                  const group = entry.group
                  // Backend projection uses the same name ordering as injection,
                  // including disabled members for editing but not model context.
                  const ids = live.preview?.custom_item_ids[group.id] ?? []
                  return <LoreIndexSection key={entry.key} {...shared} group={group} items={items} members={groupItems(entry.key, ids).filter(item => item.index_memberships?.some(m => m.group_id === group.id))} mode={mode}
                    onChange={patch => changeGroup(group.id, patch)} onRemove={() => setRemoveGroup(group)}
                    onLinkItems={entries => updateItems(entries, { kind: 'membership', groupID: group.id, detail: 'inherit' })} onSave={() => void document.flush()} />
                }
                const group = entry.group
                const category = sections.find(section => section.id === group.category_id)
                const title = group.load_mode === 'resident' ? t('lore.index.loadMode.resident') : `${category?.name || t(category?.labelKey ?? 'lore.type.world')} · ${t(`lore.index.loadMode.${group.load_mode}`)}`
                return <LoreIndexAutomaticSection key={entry.key} {...shared} groupKey={entry.key} title={title} items={groupItems(entry.key, group.item_ids)}
                  detail={guide.automatic_details?.[group.key] ?? group.default_detail}
                  onDetail={detail => document.setGuide({ ...guide, automatic_details: { ...guide.automatic_details, [group.key]: detail } })} />
              })}
            </LoreIndexSortArea>
          </section>
        </article>
      </div>
      <ScrollToTopButton scrollRef={editor} />
      </div>
      </CollapsibleResizablePanel>
      <CollapsiblePanelSeparator
        visible={!compact && previewVisible} aria-label={t('lore.index.resizePreview')}
        className="nova-resize-handle nova-resize-divider nova-resize-divider-vertical relative z-30 -mx-1 w-2 shrink-0 touch-none cursor-col-resize select-none"
        {...panelLayout.resizeHandleIntentProps}
      />
      <CollapsibleResizablePanel
        id="lore-index-preview-pane" visible={compact ? pane === 'preview' : previewVisible} side="right"
        minSize={compact ? (pane === 'preview' ? '100%' : '0px') : '280px'} defaultSize="48%" restorationKey={compact ? 'compact' : 'wide'}
        initialExpandSize={compact ? '100%' : resolvePanelInitialSize(wideLayout, 'lore-index-preview-pane', '48%')}
      >
        <LoreIndexPreview {...live} />
      </CollapsibleResizablePanel>
    </PanelMotionGroup>
    <ConfirmDialog open={Boolean(removeGroup)} onOpenChange={open => { if (!open) setRemoveGroup(null) }} title={t('lore.index.removeSection', { name: removeGroup?.name })} description={t('lore.index.removeHint')} confirmLabel={t('common.delete')} tone="danger" onConfirm={() => { document.setGuide({ ...guide, groups: guide.groups.filter(g => g.id !== removeGroup?.id) }); setRemoveGroup(null) }} />
  </div>
}
