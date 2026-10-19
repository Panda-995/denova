import { useLoreCategories } from '@/features/lore/use-lore-categories'
import { BookMarked, Database, LayoutGrid } from 'lucide-react'
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { AdaptiveSurface } from '@/components/layout/adaptive-surface'
import { ResourceDirectory } from '@/components/resource-directory/ResourceDirectory'
import { applyResourceDirectoryOrder, useResourceDirectoryOrder } from '@/components/resource-directory/use-resource-directory-order'
import type {
  ResourceDirectoryBadge,
  ResourceDirectoryItem,
  ResourceDirectorySection,
} from '@/components/resource-directory/types'
import { Button } from '@/components/ui/button'
import { EmptyState } from '@/components/common/EmptyState'
import { InlineErrorNotice } from '@/components/common/inline-error-notice'
import type { EditorFlushHandler } from '@/components/Editor/useEditorDraftPersistence'
import type {
  DocumentReviewController,
  DocumentReviewNavigationIntent,
} from '@/features/document-review/controller'
import { loreImageURL, type LoreItem } from '@/lib/api'
import { sectionItems, type KnowledgeSection } from './knowledge-sections'
import { LoreCreateEditor } from './LoreCreateEditor'
import { closeMobilePanes } from '@/components/layout/mobile-pane-events'
import { loreLoadModeLabel } from './options'
import { LoreWorkspaceEditor } from './LoreWorkspaceEditor'
import { useLoreWorkspace } from './use-lore-workspace'
import { hasLoreProtagonistTag } from './tags'
import { LoadingState } from '@/components/common/LoadingState'
import { EMPTY_LORE_FILTERS, filterLoreItems, type LoreFilters } from './lore-filters'
import { LoreFiltersButton, LoreFilterSummary } from './LoreFilters'
import type { ToolNavigationIntent } from '@/components/Chat/tool-navigation'
import { LORE_OVERVIEW_ID } from './LoreLibrary'
import { LORE_INDEX_ID } from './LoreIndexDocument'
import { LoreWorkspaceLibraryView } from './LoreWorkspaceLibraryView'

interface LoreWorkspaceTabProps {
  /** Retained tabs keep their drafts; only the active tab owns the host save guard. */
  active?: boolean
  projectId: string
  documentReview: DocumentReviewController
  navigationIntent?: DocumentReviewNavigationIntent | null
  toolNavigationIntent?: ToolNavigationIntent | null
  refreshSignal?: number
  onEditorFlushHandlerChange: (handler: EditorFlushHandler | null) => void
  onOpenLibrary?: () => void
  onReferenceItem?: (id: string) => void
  /** Hosts track the visible resource without copying editor drafts into Agent context. */
  onActiveItemChange?: (id: string | null) => void
}

/** Writing-side projection of the lore library: quick editing, review and Agent handoff. */
export function LoreWorkspaceTab({
  active = true,
  projectId,
  documentReview,
  navigationIntent,
  toolNavigationIntent,
  refreshSignal = 0,
  onEditorFlushHandlerChange,
  onOpenLibrary,
  onReferenceItem,
  onActiveItemChange,
}: LoreWorkspaceTabProps) {
  const { t } = useTranslation()
  const { sections: categorySections } = useLoreCategories(projectId, refreshSignal)
  const directoryOrder = useResourceDirectoryOrder(`nova.lore-directory-order:${projectId}`)
  const [searchQuery, setSearchQuery] = useState('')
  const [filters, setFilters] = useState<LoreFilters>(EMPTY_LORE_FILTERS)
  const [creating, setCreating] = useState<KnowledgeSection | null>(null)
  const [createdId, setCreatedId] = useState('')
  const [libraryDestination, setLibraryDestination] = useState<typeof LORE_OVERVIEW_ID | typeof LORE_INDEX_ID | null>(null)
  const indexFlush = useRef<EditorFlushHandler | null>(null)
  const itemFlush = useRef<EditorFlushHandler | null>(null)
  const handledReviewNavigation = useRef('')
  const handledToolNavigation = useRef('')
  const handleItemFlush = useCallback((handler: EditorFlushHandler | null) => { itemFlush.current = handler }, [])
  const handleIndexFlush = useCallback((handler: EditorFlushHandler | null) => { indexFlush.current = handler }, [])
  const flush = useCallback(async () => {
    if (itemFlush.current && !(await itemFlush.current())) return false
    return indexFlush.current ? indexFlush.current() : true
  }, [])
  useLayoutEffect(() => {
    if (!active) return
    // Use the editor registration phase so an outgoing tab cannot clear the new owner's handler.
    onEditorFlushHandlerChange(flush)
    return () => onEditorFlushHandlerChange(null)
  }, [active, flush, onEditorFlushHandlerChange])
  useEffect(() => { setFilters(EMPTY_LORE_FILTERS); setSearchQuery(''); setCreating(null); setCreatedId(''); setLibraryDestination(null) }, [projectId])
  const lore = useLoreWorkspace({
    active,
    projectId,
    refreshSignal,
    onFlushHandlerChange: handleItemFlush,
  })
  const visibleItemId = creating || libraryDestination ? null : lore.activeId || null
  useEffect(() => { onActiveItemChange?.(visibleItemId) }, [onActiveItemChange, visibleItemId])
  const navigationTargetID = navigationIntent
    ? documentReview.comments.find(
        (comment) => comment.id === navigationIntent.commentID,
      )?.target.id || ''
    : ''
  const startCreating = useCallback(async (section: KnowledgeSection) => {
    if (!(await flush())) return
    setCreating(section)
    setCreatedId('')
    closeMobilePanes()
  }, [flush])
  const selectDestination = useCallback(async (id: string) => {
    if (!(await flush())) return
    if (id === LORE_OVERVIEW_ID || id === LORE_INDEX_ID) {
      setLibraryDestination(id)
    } else {
      if (!(await lore.selectItem(id))) return
      setLibraryDestination(null)
    }
    setCreating(null)
    setCreatedId('')
    closeMobilePanes()
    console.info('[lore-workspace] destination selected', { projectId, destination: id })
  }, [flush, lore.selectItem, projectId])
  useEffect(() => {
    const key = `${projectId}:${navigationIntent?.nonce}`
    if (!active || !navigationTargetID || handledReviewNavigation.current === key || !lore.items.some(item => item.id === navigationTargetID)) return
    handledReviewNavigation.current = key
    void selectDestination(navigationTargetID)
  }, [active, lore.items, navigationIntent?.nonce, navigationTargetID, projectId, selectDestination])
  useEffect(() => {
    const key = `${projectId}:${toolNavigationIntent?.nonce}`
    if (!active || handledToolNavigation.current === key) return
    const target = toolNavigationIntent?.target
    if (!target || target.kind !== 'lore_item') return
    const targetID = target.id || lore.items.find((item) => item.name === target.name)?.id || ''
    if (!targetID || !lore.items.some(item => item.id === targetID)) return
    handledToolNavigation.current = key
    void selectDestination(targetID)
  }, [active, lore.items, projectId, selectDestination, toolNavigationIntent])
  const filteredItems = useMemo(() => filterLoreItems(lore.items, filters, searchQuery, projectId), [lore.items, filters, searchQuery, projectId])
  const sections = useMemo<ResourceDirectorySection[]>(
    () =>
      categorySections.filter((section) => filters.category === 'all' || section.id === filters.category).map((section) => ({
        id: section.id,
        label: (section.name || t(section.labelKey)),
        icon: section.icon,
        reorderable: true,
        items: sectionItems(filteredItems, section).map((item) =>
          loreDirectoryItem(item, projectId, t),
        ),
        onCreate: () => { void startCreating(section) },
        createLabel: t('loreWorkspace.createInSection', {
          section: (section.name || t(section.labelKey)),
        }),
      })),
    [startCreating, filteredItems, filters.category, projectId, t, categorySections],
  )

  const directory = (
    <div className="nova-sidebar flex h-full min-h-0 flex-col bg-[var(--nova-surface-2)]">
      {lore.loading && lore.items.length === 0 ? (
        <LoadingState label={t('common.loading')} variant="panel" className="h-full min-h-0" />
      ) : lore.error && lore.items.length === 0 ? (
        <div className="grid gap-2 p-3">
          <InlineErrorNotice message={lore.error} />
          <Button
            variant="outline"
            size="sm"
            onClick={() => {
              void lore.reload(lore.activeId)
            }}
          >
            {t('common.retry')}
          </Button>
        </div>
      ) : (
        <ResourceDirectory
          sections={applyResourceDirectoryOrder(sections, directoryOrder.order)}
          showExpandCollapseAll
          activeId={creating ? null : libraryDestination || lore.activeId || null}
          pinnedEntries={[
            { id: LORE_OVERVIEW_ID, label: t('lore.library.title'), icon: LayoutGrid },
            { id: LORE_INDEX_ID, label: t('lore.index.title'), icon: BookMarked },
          ]}
          onSelect={id => void selectDestination(id)}
          onReorderItems={(sectionId, orderedItemIds) => directoryOrder.reorderItems(sectionId, orderedItemIds, lore.items.filter(item => item.type === sectionId).map(item => item.id))}
          saving={lore.autosaveStatus === 'saving'}
          searchPlaceholder={t('loreWorkspace.search')}
          query={searchQuery}
          onQueryChange={setSearchQuery}
          filterItem={() => true}
          headerActions={<LoreFiltersButton projectId={projectId} presentation="icon" items={lore.items} filters={filters} onChange={setFilters} />}
          searchDetails={<LoreFilterSummary projectId={projectId} filters={filters} onChange={setFilters} query={searchQuery} onQueryChange={setSearchQuery} matched={filteredItems.length} total={lore.items.length} />}
          headerContent={lore.error ? <InlineErrorNotice message={lore.error} /> : undefined}
          emptyContent={
            <div className="px-2 py-8 text-center text-xs text-[var(--nova-text-faint)]">
              {t('loreWorkspace.emptyDirectory')}
            </div>
          }
        />
      )}
    </div>
  )

  return (
    <section
      className="h-full min-h-0 min-w-0 bg-[var(--nova-bg)]"
      aria-label={t('loreWorkspace.title')}
    >
      <AdaptiveSurface
        left={{
          id: 'writing-lore-directory',
          title: t('loreWorkspace.directoryTitle'),
          side: 'left',
          icon: <BookMarked className="h-4 w-4 text-[var(--nova-success)]" />,
          content: directory,
          desktopClassName: 'min-h-0 border-r border-[var(--nova-border)]',
          mobileClassName: 'w-[min(88vw,340px)]',
        }}
        leftResize={{
          layoutKey: 'nova-writing-lore-directory-layout',
          label: t('layout.resize.sidebar'),
          defaultSize: '240px',
          minSize: '200px',
          maxSize: '36%',
        }}
        collapseAt={720}
        mobilePaneScope="surface"
      >
        {({ isMobile, openLeft }) => (
          <>
          <LoreWorkspaceLibraryView key={projectId} projectId={projectId}
            activeId={creating ? null : libraryDestination} items={lore.items} filteredItems={filteredItems}
            filters={filters} onFiltersChange={setFilters} query={searchQuery} onQueryChange={setSearchQuery}
            onSelect={id => void selectDestination(id)} onCreate={section => void startCreating(section)}
            onReload={() => lore.reload(lore.activeId)} onFlush={flush} onIndexFlushHandlerChange={handleIndexFlush}
            onOpenDirectory={isMobile ? openLeft : undefined} onOpenLibrary={onOpenLibrary} />
          {creating ? (
            <LoreCreateEditor
              key={`${projectId}:${creating.id}`}
              projectId={projectId}
              category={creating.createType}
              categoryLabel={creating.name || t(creating.labelKey)}
              importance="important"
              loadMode="auto"
              items={lore.items}
              onCancel={() => setCreating(null)}
              onCreated={(item) => {
                setCreatedId(item.id)
                setCreating(null)
                setLibraryDestination(null)
                lore.acceptCreatedItem(item)
              }}
            />
          ) : libraryDestination ? null : lore.loading && !lore.draft ? (
            <LoadingState label={t('common.loading')} className="h-full min-h-0" />
          ) : lore.error && lore.items.length === 0 ? (
            <div className="grid h-full place-content-center gap-3 px-6">
              <InlineErrorNotice message={lore.error} />
              <Button variant="outline" size="sm" onClick={() => void lore.reload()}>
                {t('common.retry')}
              </Button>
            </div>
          ) : lore.draft ? (
            <LoreWorkspaceEditor
              autoFocusContent={createdId === lore.draft.id}
              projectId={projectId}
              draft={lore.draft}
              items={lore.items}
              tagDraft={lore.tagDraft}
              autosaveStatus={lore.autosaveStatus}
              autosaveError={lore.autosaveError}
              documentReview={documentReview}
              navigationIntent={
                navigationTargetID === lore.activeId ? navigationIntent : null
              }
              highlightQuery={searchQuery}
              onDraftChange={lore.setDraft}
              onSelectItem={id => void selectDestination(id)}
              onTagDraftChange={lore.setTagDraft}
              onPrepareSnapshot={lore.prepareSnapshot}
              onFlush={lore.flush}
              onDelete={lore.deleteItem}
              onOpenDirectory={isMobile ? openLeft : undefined}
              onOpenLibrary={onOpenLibrary}
              onReferenceItem={onReferenceItem}
            />
          ) : (
            <div className="relative flex h-full min-h-0 items-center justify-center">
              {isMobile ? (
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  onClick={openLeft}
                  className="absolute left-3 top-3"
                >
                  <BookMarked />
                  {t('loreWorkspace.openDirectory')}
                </Button>
              ) : null}
              <EmptyState
                icon={Database}
                title={t('loreWorkspace.emptyTitle')}
                description={t('loreWorkspace.emptyDescription')}
                action={{
                  label: t('loreWorkspace.emptyAction'),
                  onClick: () => {
                    void startCreating(categorySections[0])
                  },
                }}
                variant="page"
              />
            </div>
          )}
          </>
        )}
      </AdaptiveSurface>
    </section>
  )
}

function loreDirectoryItem(
  item: LoreItem,
  projectId: string,
  t: (key: string) => string,
): ResourceDirectoryItem {
  const imageSrc = loreImageURL(projectId, item)
  const badges: ResourceDirectoryBadge[] = [{
    label:
      item.load_mode === 'resident'
        ? t('settingPanel.lore.loadModeBadge.resident')
        : t('settingPanel.lore.loadModeBadge.onDemand'),
    title: loreLoadModeLabel(item.load_mode, t),
    tone: item.load_mode === 'resident' ? 'default' : 'outline',
  }]
  if (item.type === 'character' && hasLoreProtagonistTag(item.tags || [])) {
    badges.unshift({ label: t('loreWorkspace.protagonistTag'), tone: 'warning' })
  }
  return {
    id: item.id,
    title: item.name,
    thumbnailUrl: imageSrc || null,
    disabled: item.enabled === false,
    searchText: `${(item.tags || []).join(' ')} ${(item.keywords || []).join(' ')} ${item.content || ''}`,
    badges,
  }
}
