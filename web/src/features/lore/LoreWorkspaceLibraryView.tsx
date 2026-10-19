import { useState } from 'react'
import { BookMarked, LayoutGrid, LibraryBig } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import type { LoreItem, LoreItemImageGenerateRequest } from '@/lib/api'
import type { EditorFlushHandler } from '@/components/Editor/useEditorDraftPersistence'
import { ConfigManagerChat } from '@/components/Chat/ConfigManagerChat'
import { FeaturePageShell } from '@/components/layout/feature-page-shell'
import { MobilePaneTrigger } from '@/components/layout/mobile-pane-trigger'
import { Button } from '@/components/ui/button'
import { Sheet, SheetContent, SheetHeader, SheetTitle } from '@/components/ui/sheet'
import { LoreClassificationDialog } from '@/features/interactive/components/LoreClassificationDialog'
import { LoreLibrary, LORE_OVERVIEW_ID } from './LoreLibrary'
import { LoreIndexDocument, LORE_INDEX_ID } from './LoreIndexDocument'
import type { KnowledgeSection } from './knowledge-sections'
import type { LoreFilters } from './lore-filters'
import { loreImageTaskInstruction, type LoreBatchImageMode } from './lore-image-task'
import { notifyLoreUpdated } from './events'

/** Library destinations inside writing; mutations use the same library components
 * and configuration Agent, while the workspace owns navigation and draft flushing. */
export function LoreWorkspaceLibraryView({
  projectId, activeId, items, filteredItems, filters, onFiltersChange, query, onQueryChange,
  onSelect, onCreate, onReload, onFlush, onIndexFlushHandlerChange, onOpenDirectory, onOpenLibrary,
}: {
  projectId: string
  activeId: typeof LORE_OVERVIEW_ID | typeof LORE_INDEX_ID | null
  items: LoreItem[]
  filteredItems: LoreItem[]
  filters: LoreFilters
  onFiltersChange: (filters: LoreFilters) => void
  query: string
  onQueryChange: (query: string) => void
  onSelect: (id: string) => void
  onCreate: (section: KnowledgeSection) => void
  onReload: () => Promise<void>
  onFlush: EditorFlushHandler
  onIndexFlushHandlerChange: (handler: EditorFlushHandler | null) => void
  onOpenDirectory?: () => void
  onOpenLibrary?: () => void
}) {
  const { t } = useTranslation()
  const [indexActions, setIndexActions] = useState<HTMLDivElement | null>(null)
  const [classificationOpen, setClassificationOpen] = useState(false)
  const [agentOpen, setAgentOpen] = useState(false)
  const [imageTask, setImageTask] = useState<{ key: string; instruction: string } | null>(null)
  const isOverview = activeId === LORE_OVERVIEW_ID
  const isIndex = activeId === LORE_INDEX_ID
  const generate = async (ids: string[], request: LoreItemImageGenerateRequest, mode: LoreBatchImageMode) => {
    if (!(await onFlush())) return false
    setImageTask({ key: `lore-images-${Date.now()}`, instruction: loreImageTaskInstruction(ids, request, mode) })
    setAgentOpen(true)
    return true
  }

  return (
    <div className={isOverview || isIndex ? 'h-full min-h-0 min-w-0' : 'hidden'}>
      <FeaturePageShell icon={isIndex ? BookMarked : LayoutGrid}
        title={t(isIndex ? 'lore.index.title' : 'lore.library.title')}
        subtitle={isIndex ? undefined : t('lore.library.subtitle')}
        onSaveShortcut={onFlush}
        actions={(
          <>
            {onOpenDirectory && <MobilePaneTrigger side="left" appearance="compact" label={t('loreWorkspace.openDirectory')} onClick={onOpenDirectory} />}
            {isIndex && <Button type="button" variant="ghost" size="icon-sm" aria-label={t('lore.library.back')} onClick={() => onSelect(LORE_OVERVIEW_ID)}><LayoutGrid /></Button>}
            {isIndex && <div ref={setIndexActions} className="contents" />}
            {onOpenLibrary && <Button type="button" variant="ghost" size="icon-sm" aria-label={t('loreWorkspace.openLibrary')} onClick={onOpenLibrary}><LibraryBig /></Button>}
          </>
        )}>
        <div className={isOverview ? 'h-full min-h-0' : 'hidden'}>
          <LoreLibrary projectId={projectId} items={items} filteredItems={filteredItems}
            filters={filters} onFiltersChange={onFiltersChange} query={query} onQueryChange={onQueryChange}
            onSelect={onSelect} onCreate={onCreate} onChanged={() => void onReload()}
            onReload={onReload} onGenerate={generate}
            onOrganizeTypes={() => setClassificationOpen(true)} organizingDisabled={false} />
        </div>
        {isIndex && <LoreIndexDocument projectId={projectId} items={items} headerActionsTarget={indexActions}
          onSelect={onSelect} onChanged={() => void onReload()} onFlushHandlerChange={onIndexFlushHandlerChange} />}
      </FeaturePageShell>
      <LoreClassificationDialog open={classificationOpen} projectId={projectId} onOpenChange={setClassificationOpen}
        onApplied={() => { void onReload(); notifyLoreUpdated({ projectId }) }} />
      <Sheet open={agentOpen} onOpenChange={setAgentOpen}>
        <SheetContent className="flex w-full flex-col gap-0 sm:max-w-lg">
          <SheetHeader><SheetTitle>{t('settingPanel.loreAgent.title')}</SheetTitle></SheetHeader>
          <div className="min-h-0 flex-1">
            <ConfigManagerChat projectId={projectId} origin="lore" resourceId="lore"
              context={{ item_count: String(items.length) }} initialInstruction={imageTask?.instruction}
              initialInstructionKey={imageTask?.key} onInitialInstructionAccepted={() => setImageTask(null)}
              onMutated={() => { void onReload(); notifyLoreUpdated({ projectId }) }} />
          </div>
        </SheetContent>
      </Sheet>
    </div>
  )
}
