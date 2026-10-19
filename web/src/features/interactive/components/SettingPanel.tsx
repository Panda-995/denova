import { LoreIndexDocument, LORE_INDEX_ID } from "@/features/lore/LoreIndexDocument"
import { useLoreCategories } from '@/features/lore/use-lore-categories'
import { ResourceExchangeActions } from '@/features/market/ResourceExchangeActions'
import { closeMobilePanes } from '@/components/layout/mobile-pane-events'
import { useCallback, useEffect, useMemo, useRef, useState, type ComponentProps } from 'react'
import { BookMarked, Bot, Database, LayoutGrid, SlidersHorizontal, Sparkles, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from '@/lib/toast'
import { APIError, deleteProjectLoreItem, getProjectLoreItems, loreImageURL, readOptionalProjectFile, readProjectFile, type LoreItem } from '@/lib/api'
import { rebaseJSONValue, rebaseText } from '@/lib/three-way-rebase'
import { rebaseJSONWithRecovery, rebaseTextWithRecovery } from '@/lib/autosave/rebase-with-recovery'
import { Button } from '@/components/ui/button'
import { ConfigManagerChat } from '@/components/Chat/ConfigManagerChat'
import { ConfigManagerToggle } from '@/components/Chat/ConfigManagerToggle'
import { ConfirmDialog } from '@/components/common/ConfirmDialog'
import { EmptyState } from '@/components/common/EmptyState'
import { InlineErrorNotice } from '@/components/common/inline-error-notice'
import { LoadingState } from '@/components/common/LoadingState'
import { AutosaveStatusIndicator } from '@/components/forms/autosave-status'
import { ResourceWorkspace, useResponsiveAgentOpen } from '@/components/layout/resource-workspace'
import { FeaturePageShell } from '@/components/layout/feature-page-shell'
import { ResourceDirectory } from '@/components/resource-directory/ResourceDirectory'
import { applyResourceDirectoryOrder, useResourceDirectoryOrder } from '@/components/resource-directory/use-resource-directory-order'
import type { ResourceDirectoryBadge, ResourceDirectoryItem, ResourceDirectorySection } from '@/components/resource-directory/types'
import { INTERACTIVE_OPENING_PRESET_PATH, INTERACTIVE_OPENING_PRESET_UPDATED_EVENT, INTERACTIVE_OPENING_PRESET_ENTRY_ID, LEGACY_INTERACTIVE_OPENING_PRESET_PATH, parseBookOpeningPresets, serializeBookOpeningPresets, type BookOpeningPreset } from '../opening'
import type { GamePlanningTemplate, ImagePreset, Teller } from '../types'
import { CreatorDirectory, CreatorEditor } from './setting-panel/CreatorEditor'
import { LoreEditor } from './setting-panel/LoreEditor'
import { LoreLibrary, LORE_OVERVIEW_ID } from '@/features/lore/LoreLibrary'
import { LoreCreateEditor } from '@/features/lore/LoreCreateEditor'
import { loreImageTaskInstruction } from '@/features/lore/lore-image-task'
import { OpeningPresetEditor } from './setting-panel/OpeningPresetEditor'
import { loreImportanceLabel, loreLoadModeLabel, loreTypeLabel } from '@/features/lore/options'
import { LoreClassificationDialog } from './LoreClassificationDialog'
import { presetIconActionClassName as iconActionClassName } from './preset-config/editor-styles'
import { PresetSettingsPanel } from './setting-panel/PresetSettingsPanel'
import { loreAutosaveDraft, useLoreItemAutosave, type LoreAutosaveDraft } from '@/features/lore/use-lore-item-autosave'
import { hasLoreProtagonistTag } from '@/features/lore/tags'
import { LORE_UPDATED_EVENT, notifyLoreUpdated, type LoreUpdatedDetail } from '@/features/lore/events'
import { useProjectFileAutosave } from './setting-panel/use-project-file-autosave'
import { EMPTY_IMAGE_PRESETS, EMPTY_STORY_DIRECTORS, EMPTY_TELLERS } from './setting-panel/presetResources'
import { sectionItems, type KnowledgeSection } from '@/features/lore/knowledge-sections'
import { EMPTY_LORE_FILTERS, filterLoreItems, type LoreFilters } from '@/features/lore/lore-filters'
import { LoreFiltersButton, LoreFilterSummary } from '@/features/lore/LoreFilters'
import { isProjectChangeForProject, workspaceChangePaths, type WorkspaceChangeEvent } from '@/features/changes/types'
import { isLoreItemsPath } from '@/lib/workspace-path'
import type { DocumentReviewController, DocumentReviewNavigationIntent } from '@/features/document-review/controller'
import type { DocumentReviewSnapshot } from '@/components/Editor/documentReviewAnchors'
import type { ToolNavigationIntent } from '@/components/Chat/tool-navigation'

const CREATOR_PATH = 'CREATOR.md'
const CREATOR_ENTRY_ID = '__creator__'
const UTF8_ENCODER = new TextEncoder()

export type SettingPanelMode = 'lore' | 'creator' | 'teller'

interface SettingPanelProps {
  mode?: SettingPanelMode
  projectId: string
  tellers?: Teller[]
  storyDirectors?: GamePlanningTemplate[]
  imagePresets?: ImagePreset[]
  onTellersChange?: (tellers: Teller[]) => void
  onStoryDirectorsChange?: (directors: GamePlanningTemplate[]) => void
  onImagePresetsChange?: (presets: ImagePreset[]) => void
  documentReview?: DocumentReviewController
  documentReviewNavigationIntent?: DocumentReviewNavigationIntent | null
  refreshSignal?: number
  embedded?: boolean
  onFlushHandlerChange?: (handler: (() => Promise<boolean>) | null) => void
  toolNavigationIntent?: ToolNavigationIntent | null
}

export function SettingPanel({
  mode,
  projectId,
  tellers = EMPTY_TELLERS,
  storyDirectors = EMPTY_STORY_DIRECTORS,
  imagePresets = EMPTY_IMAGE_PRESETS,
  onTellersChange,
  onStoryDirectorsChange,
  onImagePresetsChange,
  documentReview,
  documentReviewNavigationIntent,
  refreshSignal = 0,
  embedded = false,
  onFlushHandlerChange,
  toolNavigationIntent,
}: SettingPanelProps) {
  const activeMode = mode || 'lore'
  if (activeMode === 'teller') {
    return (
      <PresetSettingsPanel
        projectId={projectId}
        tellers={tellers}
        storyDirectors={storyDirectors}
        imagePresets={imagePresets}
        onTellersChange={onTellersChange}
        onStoryDirectorsChange={onStoryDirectorsChange}
        onImagePresetsChange={onImagePresetsChange}
        embedded={embedded}
        toolNavigationIntent={toolNavigationIntent}
      />
    )
  }
  return <LoreSettingPanel mode={activeMode} projectId={projectId} documentReview={documentReview} documentReviewNavigationIntent={documentReviewNavigationIntent} refreshSignal={refreshSignal} embedded={embedded} onFlushHandlerChange={onFlushHandlerChange} toolNavigationIntent={toolNavigationIntent} />
}

function LoreSettingPanel({
  mode,
  projectId,
  documentReview,
  documentReviewNavigationIntent,
  refreshSignal,
  embedded,
  onFlushHandlerChange,
  toolNavigationIntent,
}: {
  mode: Exclude<SettingPanelMode, 'teller'>
  projectId: string
  documentReview?: DocumentReviewController
  documentReviewNavigationIntent?: DocumentReviewNavigationIntent | null
  refreshSignal: number
  embedded: boolean
  onFlushHandlerChange?: (handler: (() => Promise<boolean>) | null) => void
  toolNavigationIntent?: ToolNavigationIntent | null
}) {
  const { t } = useTranslation()
  const { sections: categorySections } = useLoreCategories(projectId, refreshSignal)
  const directoryOrder = useResourceDirectoryOrder(`nova.lore-directory-order:${projectId}`)
  const activeMode = mode
  const [items, setItems] = useState<LoreItem[]>([])
  const [loading, setLoading] = useState(Boolean(projectId))
  const [loadError, setLoadError] = useState<string | null>(null)
  const [activeId, setActiveId] = useState(LORE_OVERVIEW_ID)
  const indexFlush = useRef<(() => Promise<boolean>) | null>(null)
  const [indexHeaderActionsTarget, setIndexHeaderActionsTarget] = useState<HTMLDivElement | null>(null)
  const handleIndexFlush = useCallback((handler: (() => Promise<boolean>) | null) => { indexFlush.current = handler }, [])
  const [creating, setCreating] = useState<KnowledgeSection | null>(null)
  const [createdId, setCreatedId] = useState('')
  const [draft, setDraft] = useState<LoreItem | null>(null)
  const [tagDraft, setTagDraft] = useState('')
  const [query, setQuery] = useState('')
  const [filters, setFilters] = useState<LoreFilters>(EMPTY_LORE_FILTERS)
  const filteredItems = useMemo(() => filterLoreItems(items, filters, query, projectId), [items, filters, query, projectId])
  const [creatorContent, setCreatorContent] = useState('')
  const [creatorRevision, setCreatorRevision] = useState('')
  const [creatorProjectId, setCreatorProjectId] = useState('')
  const [openingPresets, setOpeningPresets] = useState<BookOpeningPreset[]>([])
  const [openingPresetRevision, setOpeningPresetRevision] = useState('')
  const [openingPresetProjectId, setOpeningPresetProjectId] = useState('')
  const [activeOpeningPresetId, setActiveOpeningPresetId] = useState('')
  const [loreClassificationOpen, setLoreClassificationOpen] = useState(false)
  const [pendingLoreImageTask, setPendingLoreImageTask] = useState<{ key: string; instruction: string } | null>(null)
  const [agentOpen, setAgentOpen] = useResponsiveAgentOpen()
  const [deleteLoreTarget, setDeleteLoreTarget] = useState<LoreItem | null>(null)
  const [saving, setSaving] = useState(false)
  const loreDraftRef = useRef<LoreItem | null>(null)
  const loreTagDraftRef = useRef('')
  const loreBaselineDraftRef = useRef<LoreAutosaveDraft | null>(null)
  const creatorContentRef = useRef('')
  const creatorBaselineContentRef = useRef('')
  const creatorBaselineRevisionRef = useRef('')
  const openingPresetsRef = useRef<BookOpeningPreset[]>([])
  const openingPresetBaselineContentRef = useRef('')
  const openingPresetBaselineRevisionRef = useRef('')
  const loreRebaseSequenceRef = useRef(0)
  const refreshSignalRef = useRef(refreshSignal)
  const isCreatorActive = activeMode === 'creator' || (activeMode === 'lore' && activeId === CREATOR_ENTRY_ID)
  const documentReviewLoreID = useMemo(() => {
    if (!documentReview || !documentReviewNavigationIntent) return ''
    const target = documentReview.comments.find((comment) => comment.id === documentReviewNavigationIntent.commentID)?.target
    return target?.kind === 'lore_item' ? target.id : ''
  }, [documentReview, documentReviewNavigationIntent])
  creatorContentRef.current = creatorContent
  openingPresetsRef.current = openingPresets

  const selectedLoreBaseline = useMemo<LoreAutosaveDraft | null>(() => {
    const item = items.find((entry) => entry.id === activeId)
    return item ? loreAutosaveDraft(item) : null
  }, [activeId, items])
  const residentLoreBytes = useMemo(() => {
    const persistedBytes = items
      .filter((item) => item.id !== draft?.id && item.enabled !== false && item.load_mode === 'resident')
      .reduce((total, item) => total + UTF8_ENCODER.encode((item.content || '').trim()).length, 0)
    if (draft?.enabled === false || draft?.load_mode !== 'resident') return persistedBytes
    return persistedBytes + UTF8_ENCODER.encode((draft.content || '').trim()).length
  }, [draft, items])
  const loreAutosave = useLoreItemAutosave({
    draft,
    tagDraft,
    baseline: selectedLoreBaseline,
    active: activeMode === 'lore'
      && Boolean(draft)
      && activeId !== LORE_OVERVIEW_ID
      && activeId !== CREATOR_ENTRY_ID
      && activeId !== INTERACTIVE_OPENING_PRESET_ENTRY_ID,
    projectId,
    onSaved: (item, submitted) => {
      setItems((current) => current.map((entry) => entry.id === item.id ? item : entry))
      const currentDraft = loreDraftRef.current
      const savedBaseline = loreAutosaveDraft(item)
      const currentAutosaveDraft = currentDraft?.id === item.id
        ? { ...currentDraft, tags: [...(currentDraft.tags || [])], tag_draft: loreTagDraftRef.current }
        : submitted
      const rebased = rebaseJSONValue(submitted, currentAutosaveDraft, savedBaseline)
      const { tag_draft: nextTagDraft, ...nextDraft } = rebased
      setDraft(nextDraft)
      setTagDraft(nextTagDraft)
      loreBaselineDraftRef.current = savedBaseline
    },
    onAutoSaveError: (error) => {
      console.warn('[lore-editor] failed to autosave lore item', error)
      toast.error(error instanceof Error ? error.message : t('editor.saveFailed'))
    },
  })

  const creatorAutosave = useProjectFileAutosave({
    projectId,
    path: CREATOR_PATH,
    content: creatorContent,
    revision: creatorRevision,
    fileProjectId: creatorProjectId,
    active: isCreatorActive,
    onSaved: (saved, submitted) => {
      if (saved.project_id !== projectId) return
      creatorBaselineContentRef.current = saved.content
      creatorBaselineRevisionRef.current = saved.updated_at || ''
      setCreatorContent((current) => current === submitted.content ? saved.content : current)
      setCreatorRevision(saved.updated_at || '')
    },
    onAutoSaveError: (error) => {
      console.error('[creator-editor] failed to autosave CREATOR.md', error)
      toast.error((error as Error).message || t('editor.saveFailed'))
    },
  })

  const openingPresetAutosave = useProjectFileAutosave({
    projectId,
    path: INTERACTIVE_OPENING_PRESET_PATH,
    content: serializeBookOpeningPresets(openingPresets),
    revision: openingPresetRevision,
    fileProjectId: openingPresetProjectId,
    active: activeMode === 'lore' && activeId === INTERACTIVE_OPENING_PRESET_ENTRY_ID,
    onSaved: (saved, submitted) => {
      if (saved.project_id !== projectId) return
      openingPresetBaselineContentRef.current = saved.content
      openingPresetBaselineRevisionRef.current = saved.updated_at || ''
      setOpeningPresets((current) => (
        serializeBookOpeningPresets(current) === submitted.content
          ? parseBookOpeningPresets(saved.content)
          : current
      ))
      setOpeningPresetRevision(saved.updated_at || '')
      notifyOpeningPresetUpdated()
    },
    onAutoSaveError: (error) => {
      console.error('[opening-preset-editor] failed to autosave opening presets', error)
      toast.error((error as Error).message || t('editor.saveFailed'))
    },
  })

  const reconcileCreatorFile = useCallback(async (file: Awaited<ReturnType<typeof readProjectFile>>) => {
    if (file.project_id !== projectId) return
    const fileContent = file.content || ''
    const previousBaseline = creatorBaselineContentRef.current
    const previousRevision = creatorBaselineRevisionRef.current
    const capturedDraft = creatorContentRef.current
    let rebasedContent = await rebaseTextWithRecovery({
      resource: 'project_file',
      scope: file.project_id,
      id: CREATOR_PATH,
      baseline: { revision: previousRevision, value: previousBaseline },
      local: { revision: previousRevision, value: capturedDraft },
      external: { revision: file.revision, value: fileContent },
    })
    if (creatorContentRef.current !== capturedDraft) {
      rebasedContent = rebaseText(capturedDraft, creatorContentRef.current, rebasedContent)
    }
    creatorAutosave.resetBaseline({
      id: CREATOR_PATH,
      content: fileContent,
      project_id: file.project_id,
      updated_at: file.revision || '',
    })
    creatorBaselineContentRef.current = fileContent
    creatorBaselineRevisionRef.current = file.revision || ''
    setCreatorContent(rebasedContent)
    setCreatorRevision(file.revision || '')
    setCreatorProjectId(file.project_id)
  }, [creatorAutosave.resetBaseline, projectId])

  const reconcileOpeningPresetFile = useCallback(async (file: Awaited<ReturnType<typeof readProjectFile>>) => {
    if (file.project_id !== projectId) return
    const nextPresets = parseBookOpeningPresets(file.content || '')
    const nextContent = serializeBookOpeningPresets(nextPresets)
    const currentContent = serializeBookOpeningPresets(openingPresetsRef.current)
    const previousRevision = openingPresetBaselineRevisionRef.current
    let rebasedContent = await rebaseTextWithRecovery({
      resource: 'project_file',
      scope: file.project_id,
      id: INTERACTIVE_OPENING_PRESET_PATH,
      baseline: { revision: previousRevision, value: openingPresetBaselineContentRef.current },
      local: { revision: previousRevision, value: currentContent },
      external: { revision: file.revision, value: nextContent },
    })
    const latestCurrentContent = serializeBookOpeningPresets(openingPresetsRef.current)
    if (latestCurrentContent !== currentContent) {
      rebasedContent = rebaseText(currentContent, latestCurrentContent, rebasedContent)
    }
    const rebasedPresets = parseBookOpeningPresets(rebasedContent)
    openingPresetAutosave.resetBaseline({
      id: INTERACTIVE_OPENING_PRESET_PATH,
      content: nextContent,
      project_id: file.project_id,
      updated_at: file.revision || '',
    })
    openingPresetBaselineContentRef.current = nextContent
    openingPresetBaselineRevisionRef.current = file.revision || ''
    setOpeningPresets(rebasedPresets)
    setOpeningPresetRevision(file.revision || '')
    setOpeningPresetProjectId(file.project_id)
    setActiveOpeningPresetId((current) => (
      current && rebasedPresets.some((preset) => preset.id === current)
        ? current
        : rebasedPresets[0]?.id || ''
    ))
  }, [openingPresetAutosave.resetBaseline, projectId])

  const loadLoreItems = useCallback(async () => {
    if (!projectId) {
      setItems([])
      setActiveId('')
      setLoadError(null)
      setLoading(false)
      return
    }
    setLoading(true)
    setLoadError(null)
    try {
      const data = await getProjectLoreItems(projectId)
      setItems(data)
      setActiveId((current) => current === LORE_OVERVIEW_ID || current === LORE_INDEX_ID || current === CREATOR_ENTRY_ID || current === INTERACTIVE_OPENING_PRESET_ENTRY_ID || data.some((item) => item.id === current) ? current : LORE_OVERVIEW_ID)
    } catch (error) {
      setItems([])
      setActiveId('')
      setLoadError(error instanceof Error ? error.message : String(error))
    } finally {
      setLoading(false)
    }
  }, [projectId])

  useEffect(() => {
    setItems([])
    setActiveId(LORE_OVERVIEW_ID)
    setCreating(null)
    setCreatedId('')
    setDraft(null)
    setTagDraft('')
    loreBaselineDraftRef.current = null
    setQuery('')
    setFilters(EMPTY_LORE_FILTERS)
    void loadLoreItems()
  }, [loadLoreItems])

  useEffect(() => {
    const sequence = loreRebaseSequenceRef.current + 1
    loreRebaseSequenceRef.current = sequence
    const item = items.find((entry) => entry.id === activeId) || null
    const nextBaseline = item ? loreAutosaveDraft(item) : null
    const currentDraft = loreDraftRef.current
    const previousBaseline = loreBaselineDraftRef.current
    const currentAutosaveDraft = currentDraft && item && currentDraft.id === item.id
      ? { ...currentDraft, tags: [...(currentDraft.tags || [])], tag_draft: loreTagDraftRef.current }
      : null
    void (async () => {
      if (
        currentDraft
        && previousBaseline?.id === currentDraft.id
        && !items.some((entry) => entry.id === currentDraft.id)
      ) {
        await rebaseJSONWithRecovery<LoreAutosaveDraft | null>({
          resource: 'lore_item',
          scope: projectId,
          id: currentDraft.id,
          baseline: { revision: previousBaseline.updated_at, value: previousBaseline },
          local: {
            revision: previousBaseline.updated_at,
            value: { ...currentDraft, tags: [...(currentDraft.tags || [])], tag_draft: loreTagDraftRef.current },
          },
          external: { revision: 'deleted', value: null },
        })
      }
      let rebasedFromDraft = currentDraft
      let rebasedFromTagDraft = loreTagDraftRef.current
      let rebasedFromAutosaveDraft = currentAutosaveDraft
      let rebased = nextBaseline
        ? previousBaseline?.id === nextBaseline.id && currentAutosaveDraft
          ? await rebaseJSONWithRecovery({
              resource: 'lore_item',
              scope: projectId,
              id: nextBaseline.id,
              baseline: { revision: previousBaseline.updated_at, value: previousBaseline },
              local: { revision: previousBaseline.updated_at, value: currentAutosaveDraft },
              external: { revision: nextBaseline.updated_at, value: nextBaseline },
            })
          : nextBaseline
        : null
      while (
        sequence === loreRebaseSequenceRef.current
        && rebased
        && rebasedFromAutosaveDraft?.id === rebased.id
      ) {
        const latestDraft = loreDraftRef.current
        const latestTagDraft = loreTagDraftRef.current
        if (!latestDraft || latestDraft.id !== rebased.id) break
        if (Object.is(latestDraft, rebasedFromDraft) && latestTagDraft === rebasedFromTagDraft) break
        const latestAutosaveDraft = {
          ...latestDraft,
          tags: [...(latestDraft.tags || [])],
          tag_draft: latestTagDraft,
        }
        rebased = await rebaseJSONWithRecovery({
          resource: 'lore_item',
          scope: projectId,
          id: rebased.id,
          baseline: { revision: rebasedFromAutosaveDraft.updated_at, value: rebasedFromAutosaveDraft },
          local: { revision: rebasedFromAutosaveDraft.updated_at, value: latestAutosaveDraft },
          external: { revision: nextBaseline?.updated_at, value: rebased },
        })
        rebasedFromDraft = latestDraft
        rebasedFromTagDraft = latestTagDraft
        rebasedFromAutosaveDraft = latestAutosaveDraft
      }
      if (sequence !== loreRebaseSequenceRef.current) return
      if (rebased) {
        const { tag_draft: nextTagDraft, ...nextDraft } = rebased
        setDraft(nextDraft)
        setTagDraft(nextTagDraft)
      } else {
        setDraft(null)
        setTagDraft('')
      }
      loreBaselineDraftRef.current = nextBaseline
    })().catch((error) => console.error('[lore-editor] failed to reconcile external lore update', error))
    return () => {
      if (loreRebaseSequenceRef.current === sequence) loreRebaseSequenceRef.current += 1
    }
  }, [activeId, items, projectId])

  useEffect(() => {
    loreDraftRef.current = draft
    loreTagDraftRef.current = tagDraft
  }, [draft, tagDraft])

  useEffect(() => {
    if (!isCreatorActive) return
    let cancelled = false
    creatorContentRef.current = ''
    creatorBaselineContentRef.current = ''
    creatorBaselineRevisionRef.current = ''
    setCreatorContent('')
    setCreatorRevision('')
    setCreatorProjectId('')
    if (!projectId)
      return () => {
        cancelled = true
      }
    readProjectFile(projectId, CREATOR_PATH)
      .then(async (data) => {
        if (!cancelled) await reconcileCreatorFile(data)
      })
      .catch((error) => {
        if (!cancelled) {
          const missing = error instanceof APIError && error.status === 404
          if (missing) {
            creatorAutosave.resetBaseline({
              id: CREATOR_PATH,
              content: '',
              project_id: projectId,
              updated_at: 'missing',
            })
            creatorBaselineContentRef.current = ''
            creatorBaselineRevisionRef.current = 'missing'
          }
          setCreatorContent('')
          setCreatorRevision(missing ? 'missing' : '')
          setCreatorProjectId(missing ? projectId : '')
        }
      })
    return () => {
      cancelled = true
    }
  }, [creatorAutosave.resetBaseline, isCreatorActive, projectId, reconcileCreatorFile])

  useEffect(() => {
    if (activeMode !== 'lore' || activeId !== INTERACTIVE_OPENING_PRESET_ENTRY_ID) return
    let cancelled = false
    const emptyContent = serializeBookOpeningPresets([])
    openingPresetsRef.current = []
    openingPresetBaselineContentRef.current = emptyContent
    openingPresetBaselineRevisionRef.current = ''
    setOpeningPresets([])
    setOpeningPresetRevision('')
    setOpeningPresetProjectId('')
    setActiveOpeningPresetId('')
    if (!projectId)
      return () => {
        cancelled = true
      }
    void (async () => {
      try {
        const data = await readOptionalProjectFile(projectId, INTERACTIVE_OPENING_PRESET_PATH)
        if (cancelled) return
        if (data) {
          await reconcileOpeningPresetFile(data)
          return
        }
        const legacy = await readOptionalProjectFile(projectId, LEGACY_INTERACTIVE_OPENING_PRESET_PATH)
        if (cancelled) return
        if (legacy) {
          const presets = parseBookOpeningPresets(legacy.content || '')
          const content = serializeBookOpeningPresets(presets)
          openingPresetAutosave.resetBaseline({
            id: INTERACTIVE_OPENING_PRESET_PATH,
            content,
            project_id: legacy.project_id,
            updated_at: 'missing',
          })
          openingPresetBaselineContentRef.current = content
          openingPresetBaselineRevisionRef.current = 'missing'
          setOpeningPresets(presets)
          setOpeningPresetRevision('missing')
          setOpeningPresetProjectId(legacy.project_id)
          setActiveOpeningPresetId((current) => (current && presets.some((preset) => preset.id === current) ? current : presets[0]?.id || ''))
          return
        }
        openingPresetAutosave.resetBaseline({
          id: INTERACTIVE_OPENING_PRESET_PATH,
          content: emptyContent,
          project_id: projectId,
          updated_at: 'missing',
        })
        openingPresetBaselineContentRef.current = emptyContent
        openingPresetBaselineRevisionRef.current = 'missing'
        setOpeningPresets([])
        setOpeningPresetRevision('missing')
        setOpeningPresetProjectId(projectId)
        setActiveOpeningPresetId('')
      } catch {
        if (cancelled) return
        setOpeningPresets([])
        setOpeningPresetRevision('')
        setOpeningPresetProjectId('')
        setActiveOpeningPresetId('')
      }
    })()
    return () => {
      cancelled = true
    }
  }, [activeId, activeMode, openingPresetAutosave.resetBaseline, projectId, reconcileOpeningPresetFile])

  useEffect(() => {
    const onWorkspaceChange = (event: Event) => {
      const detail = (event as CustomEvent<WorkspaceChangeEvent>).detail
      if (!isProjectChangeForProject(detail, projectId)) return
      const paths = detail?.paths
      if (isCreatorActive && (!paths || paths.includes(CREATOR_PATH))) {
        void readProjectFile(projectId, CREATOR_PATH)
          .then(reconcileCreatorFile)
          .catch((error) => console.warn('[creator-editor] failed to reload external CREATOR.md update', error))
      }
      if (
        activeMode === 'lore'
        && activeId === INTERACTIVE_OPENING_PRESET_ENTRY_ID
        && (!paths || paths.includes(INTERACTIVE_OPENING_PRESET_PATH))
      ) {
        void readProjectFile(projectId, INTERACTIVE_OPENING_PRESET_PATH)
          .then(reconcileOpeningPresetFile)
          .catch((error) => console.warn('[opening-preset-editor] failed to reload external opening preset update', error))
      }
    }
    window.addEventListener('nova:workspace-change', onWorkspaceChange)
    return () => window.removeEventListener('nova:workspace-change', onWorkspaceChange)
  }, [activeId, activeMode, isCreatorActive, projectId, reconcileCreatorFile, reconcileOpeningPresetFile])

  const refreshItems = useCallback(async () => {
    const data = await getProjectLoreItems(projectId)
    setItems(data)
    // Preserve an existing selection, including the overview, after background updates.
    setActiveId((current) => {
      if (current === LORE_OVERVIEW_ID || current === LORE_INDEX_ID || current === CREATOR_ENTRY_ID || current === INTERACTIVE_OPENING_PRESET_ENTRY_ID) return current
      if (current && data.some((item) => item.id === current)) return current
      return LORE_OVERVIEW_ID
    })
  }, [projectId])

  useEffect(() => {
    const onLoreUpdated = (event: Event) => {
      const detail = (event as CustomEvent<LoreUpdatedDetail>).detail
      if (detail?.projectId !== projectId) return
      // Mutation notifications refresh data; explicit navigation owns selection.
      void refreshItems()
    }
    const onWorkspaceChange = (event: Event) => {
      const detail = (event as CustomEvent<WorkspaceChangeEvent>).detail
      if (!isProjectChangeForProject(detail, projectId)) return
      if (detail.resync || workspaceChangePaths(detail).some(isLoreItemsPath)) {
        void refreshItems().catch(error => console.warn('[lore-index] failed to refresh externally changed items', { projectId, error }))
      }
    }
    window.addEventListener(LORE_UPDATED_EVENT, onLoreUpdated)
    window.addEventListener('nova:workspace-change', onWorkspaceChange)
    return () => {
      window.removeEventListener(LORE_UPDATED_EVENT, onLoreUpdated)
      window.removeEventListener('nova:workspace-change', onWorkspaceChange)
    }
  }, [projectId, refreshItems])

  useEffect(() => {
    if (refreshSignalRef.current === refreshSignal) return
    refreshSignalRef.current = refreshSignal
    void refreshItems()
  }, [refreshItems, refreshSignal])

  const mergeSavedLoreItem = (item: LoreItem) => {
    setItems((current) => current.map((entry) => (entry.id === item.id ? item : entry)))
    if (loreDraftRef.current?.id === item.id) {
      const { tag_draft: nextTagDraft, ...nextDraft } = loreAutosaveDraft(item)
      setDraft(nextDraft)
      setTagDraft(nextTagDraft)
      loreBaselineDraftRef.current = { ...nextDraft, tag_draft: nextTagDraft }
    }
  }

  const handleDelete = () => {
    if (!draft) return
    setDeleteLoreTarget(draft)
  }

  const confirmDeleteLoreTarget = async () => {
    if (!deleteLoreTarget) return
    setSaving(true)
    try {
      await flushLoreAutosave()
      loreAutosave.cancelPending()
      await deleteProjectLoreItem(projectId, deleteLoreTarget.id)
      await refreshItems()
      notifyLoreUpdated({ projectId, ids: [deleteLoreTarget.id] })
      setDeleteLoreTarget(null)
    } finally {
      setSaving(false)
    }
  }

  const flushLoreAutosave = useCallback(async (force = false) => {
    const pending = loreAutosave.flushPending()
    if (pending) return pending
    if (force || loreAutosave.status === 'error') return loreAutosave.saveNow(force ? 'manual' : 'auto')
    return null
  }, [loreAutosave.flushPending, loreAutosave.saveNow, loreAutosave.status])

  const flushActiveAutosave = useCallback(async () => {
    try {
      if (activeId === LORE_INDEX_ID) return await (indexFlush.current?.() ?? Promise.resolve(true))
      if (activeMode === 'creator' || (activeMode === 'lore' && activeId === CREATOR_ENTRY_ID)) {
        await (creatorAutosave.flushPending() ?? creatorAutosave.saveNow('manual'))
        return true
      }
      if (activeMode === 'lore' && activeId === INTERACTIVE_OPENING_PRESET_ENTRY_ID) {
        await (openingPresetAutosave.flushPending() ?? openingPresetAutosave.saveNow('manual'))
        return true
      }
      const item = await flushLoreAutosave()
      if (item) {
        notifyLoreUpdated({ projectId, ids: [item.id] })
      }
      return true
    } catch (err) {
      toast.error((err as Error).message || t('editor.saveFailed'))
      return false
    }
  }, [
    activeId,
    activeMode,
    creatorAutosave.flushPending,
    creatorAutosave.saveNow,
    flushLoreAutosave,
    openingPresetAutosave.flushPending,
    openingPresetAutosave.saveNow,
    projectId,
    t,
  ])

  const prepareLoreReviewSnapshot = useCallback(async (): Promise<DocumentReviewSnapshot> => {
    const itemID = loreDraftRef.current?.id
    if (!itemID || !(await flushActiveAutosave())) {
      throw new Error('The lore draft could not be saved')
    }
    const canonical = (await getProjectLoreItems(projectId)).find((item) => item.id === itemID)
    if (!canonical?.updated_at) {
      throw new Error('The canonical lore snapshot is unavailable')
    }
    setItems((current) => current.map((item) => item.id === canonical.id ? canonical : item))
    return { content: canonical.content || '', revision: canonical.updated_at }
  }, [flushActiveAutosave, projectId])

  const handleCreateLore = useCallback(async (section: KnowledgeSection = categorySections[0]) => {
    if (!(await flushActiveAutosave())) return
    setCreating(section)
    setCreatedId('')
    setActiveId('')
    closeMobilePanes()
  }, [categorySections, flushActiveAutosave])
  const handleLibraryItemChanged = useCallback((saved: LoreItem) => {
    setItems(current => current.map(item => item.id === saved.id ? saved : item))
  }, [])
  const organizeLoreTypes = useCallback(() => setLoreClassificationOpen(true), [])
  const generateLibraryImages = useCallback<ComponentProps<typeof LoreLibrary>['onGenerate']>(async (ids, request, batchMode) => {
    if (!(await flushActiveAutosave())) return false
    setPendingLoreImageTask({ key: `lore-images-${Date.now()}`, instruction: loreImageTaskInstruction(ids, request, batchMode) })
    setAgentOpen(true)
    return true
  }, [flushActiveAutosave, setAgentOpen])

  useEffect(() => {
    onFlushHandlerChange?.(flushActiveAutosave)
    return () => onFlushHandlerChange?.(null)
  }, [flushActiveAutosave, onFlushHandlerChange])

  const handleSelectLore = useCallback(async (id: string) => {
    setCreating(null)
    setCreatedId('')
    if (id === activeId) { closeMobilePanes(); return }
    try {
      if (activeId === LORE_INDEX_ID) {
        if (!(await indexFlush.current?.())) return
      } else if (activeId === CREATOR_ENTRY_ID) {
        await (creatorAutosave.flushPending() ?? creatorAutosave.saveNow('auto'))
      } else if (activeId === INTERACTIVE_OPENING_PRESET_ENTRY_ID) {
        await (openingPresetAutosave.flushPending() ?? openingPresetAutosave.saveNow('auto'))
      } else {
        await flushLoreAutosave()
      }
      setActiveId(id)
      closeMobilePanes()
    } catch (error) {
      console.error('[lore-editor] failed to flush autosave before switching resources', error)
      toast.error((error as Error).message || t('editor.saveFailed'))
    }
  }, [activeId, creatorAutosave.flushPending, creatorAutosave.saveNow, flushLoreAutosave, openingPresetAutosave.flushPending, openingPresetAutosave.saveNow, t])

  // Navigation intents are one-shot so returning to the overview stays there.
  const handledReviewNavigation = useRef<string>('')
  const handledToolNavigation = useRef<string>('')

  useEffect(() => {
    const navigationKey = `${projectId}:${documentReviewNavigationIntent?.nonce}`
    if (handledReviewNavigation.current === navigationKey) return
    if (!documentReviewLoreID || !items.some((item) => item.id === documentReviewLoreID)) return
    handledReviewNavigation.current = navigationKey
    void handleSelectLore(documentReviewLoreID)
  }, [activeId, documentReviewLoreID, documentReviewNavigationIntent?.nonce, handleSelectLore, items, projectId])

  useEffect(() => {
    const navigationKey = `${projectId}:${toolNavigationIntent?.nonce}`
    if (handledToolNavigation.current === navigationKey) return
    const target = toolNavigationIntent?.target
    if (!target || target.kind !== 'lore_item') return
    const targetID = target.id || items.find((item) => item.name === target.name)?.id || ''
    if (!targetID || !items.some((item) => item.id === targetID)) return
    handledToolNavigation.current = navigationKey
    void handleSelectLore(targetID)
  }, [activeId, handleSelectLore, items, projectId, toolNavigationIntent])

  const isIndex = !creating && activeMode === 'lore' && activeId === LORE_INDEX_ID
  const isOverview = !creating && activeMode === 'lore' && activeId === LORE_OVERVIEW_ID

  const isOpeningPresetActive = activeMode === 'lore' && activeId === INTERACTIVE_OPENING_PRESET_ENTRY_ID
  const activeAutosaveStatus = isCreatorActive
    ? creatorAutosave.status
    : isOpeningPresetActive
      ? openingPresetAutosave.status
      : loreAutosave.status
  const activeAutosaveError = isCreatorActive
    ? creatorAutosave.error
    : isOpeningPresetActive
      ? openingPresetAutosave.error
      : loreAutosave.error
  const editorHeaderIcon = isOverview ? LayoutGrid : isCreatorActive ? BookMarked : isOpeningPresetActive ? Sparkles : Database
  const editorHeaderTitle = isIndex ? t('lore.index.title') : creating ? t('lore.library.create') : isOverview ? t('lore.library.title') : isCreatorActive
      ? CREATOR_PATH
      : isOpeningPresetActive
        ? t('settingPanel.openingPreset.title')
        : editorTitle(activeMode, draft, t)
  const editorHeaderSubtitle = isIndex ? undefined : creating ? (creating.name || t(creating.labelKey)) : isOverview ? t('lore.library.subtitle') : isCreatorActive
      ? t('settingPanel.editor.creatorSubtitle')
      : isOpeningPresetActive
        ? t('settingPanel.openingPreset.subtitle')
        : editorSubtitle(draft, t, categorySections.find((section) => section.id === draft?.type)?.name)
  const loreDirectorySections = useMemo(() => applyResourceDirectoryOrder(categorySections.filter((section) => filters.category === 'all' || section.id === filters.category).map((section): ResourceDirectorySection => ({
    id: section.id,
    label: (section.name || t(section.labelKey)),
    icon: section.icon,
    reorderable: true,
    items: sectionItems(filteredItems, section).map((item) => loreItemToDirectoryItem(item, projectId, t)),
    onCreate: () => void handleCreateLore(section),
    createLabel: `${t('chat.new')}${(section.name || t(section.labelKey))}`,
  })), directoryOrder.order), [categorySections, filters.category, filteredItems, projectId, t, handleCreateLore, directoryOrder.order])
  const directoryPanel = (
    <div className="nova-sidebar flex h-full min-h-0 flex-col bg-[var(--nova-surface-2)]">
      {activeMode === 'lore' ? (
        loading ? (
          <LoadingState label={t('common.loading')} variant="panel" className="h-full min-h-0" />
        ) : loadError ? (
          <div className="flex flex-col gap-2 p-3">
            <InlineErrorNotice message={loadError} />
            <Button variant="outline" size="sm" onClick={() => void loadLoreItems()}>
              {t('common.retry')}
            </Button>
          </div>
        ) : (
          <ResourceDirectory
            sections={loreDirectorySections}
            showExpandCollapseAll
            activeId={activeId || null}
            onSelect={handleSelectLore}
            onReorderItems={(sectionId, orderedItemIds) => directoryOrder.reorderItems(sectionId, orderedItemIds, items.filter(item => item.type === sectionId).map(item => item.id))}
            saving={saving}
            pinnedEntries={[
              { id: LORE_OVERVIEW_ID, label: t('lore.library.title'), icon: LayoutGrid },
              { id: LORE_INDEX_ID, label: t('lore.index.title'), icon: BookMarked },
              { id: CREATOR_ENTRY_ID, label: CREATOR_PATH, icon: BookMarked },
              { id: INTERACTIVE_OPENING_PRESET_ENTRY_ID, label: t('settingPanel.openingPreset.title'), icon: Sparkles },
            ]}
            searchPlaceholder={t('settingPanel.searchLore')}
            query={query}
            onQueryChange={setQuery}
            filterItem={() => true}
            headerActions={<LoreFiltersButton projectId={projectId} presentation="icon" items={items} filters={filters} onChange={setFilters} />}
            searchDetails={<LoreFilterSummary projectId={projectId} filters={filters} onChange={setFilters} query={query} onQueryChange={setQuery} matched={filteredItems.length} total={items.length} />}
          />
        )
      ) : <CreatorDirectory />}
    </div>
  )

  return (
    <section className="h-full min-h-0 bg-[var(--nova-surface-2)] text-[var(--nova-text)]">
      <ResourceWorkspace
        title={panelTitle(activeMode, t)}
        embedded={embedded}
        secondaryView={{ label: t('workbench.mobile.agent'), available: true, open: agentOpen, onOpenChange: setAgentOpen }}
        left={{
          id: 'setting-directory',
          title: panelTitle(activeMode, t),
          side: 'left',
          icon: <ModeIcon mode={activeMode} />,
          content: directoryPanel,
          desktopClassName: 'min-h-0 border-r border-[var(--nova-border)]',
          mobileClassName: embedded ? 'w-[min(86vw,320px)]' : 'w-[min(90vw,360px)]',
        }}
        right={agentOpen ? {
          id: 'lore-config-manager',
          title: t('settingPanel.loreAgent.title'),
          side: 'right',
          icon: <Bot className="h-4 w-4" />,
          content: (
            <ConfigManagerChat
              projectId={projectId}
              origin="lore"
              resourceId={isIndex ? 'index' : isOverview ? 'lore' : activeId || 'lore'}
              context={{
                ...(isIndex ? { resource: 'lore_index' } : {}),
                active_lore_id: draft?.id || '',
                active_lore_name: draft?.name || '',
                item_count: String(items.length),
              }}
              initialInstruction={pendingLoreImageTask?.instruction}
              initialInstructionKey={pendingLoreImageTask?.key}
              onInitialInstructionAccepted={() => setPendingLoreImageTask(null)}
              onMutated={() => {
                void refreshItems()
                notifyLoreUpdated({ projectId })
              }}
            />
          ),
          desktopClassName: 'min-h-0 border-l border-[var(--nova-border)]',
          mobileClassName: 'w-[min(92vw,420px)]',
        } : undefined}
        className="h-full"
        mainClassName="min-h-0 min-w-0"
        leftResize={{
          layoutKey: embedded ? 'nova-embedded-setting-directory-layout' : 'nova-setting-directory-layout',
          label: t('layout.resize.sidebar'),
          defaultSize: embedded ? '224px' : '320px',
          minSize: embedded ? '180px' : '220px',
          maxSize: '42%',
        }}
        rightResize={{
          layoutKey: embedded ? 'nova-embedded-lore-config-manager-layout' : 'nova-lore-config-manager-layout',
          label: t('layout.resize.right'),
          defaultSize: '420px',
          minSize: '300px',
          maxSize: '65%',
          mainMinSize: '240px',
        }}
      >
        {() => (
          <main className="flex h-full min-h-0 min-w-0 flex-1 flex-col bg-[var(--nova-surface-2)]">
            <FeaturePageShell
              icon={editorHeaderIcon}
              title={editorHeaderTitle}
              subtitle={editorHeaderSubtitle}
              onSaveShortcut={flushActiveAutosave}
              actions={(
                <>
                  {activeMode === 'lore' && !isOverview && <Button size="icon-sm" variant="ghost" aria-label={t('lore.library.back')} onClick={() => void handleSelectLore(LORE_OVERVIEW_ID)}><LayoutGrid /></Button>}
                  <ResourceExchangeActions projectID={projectId} resources={isOpeningPresetActive && activeOpeningPresetId ? [{ kind: 'game.openings', scope: 'project', project_id: projectId, id: 'all' }] : draft && !isCreatorActive && !isOverview ? [{ kind: 'lore.collection', scope: 'project', project_id: projectId, id: 'all' }] : undefined} beforeOpen={flushActiveAutosave} onImported={async () => { await loadLoreItems(); notifyOpeningPresetUpdated() }} />
                  {!isOverview && (isCreatorActive || isOpeningPresetActive || draft) ? (
                    <AutosaveStatusIndicator
                      status={activeAutosaveStatus}
                      error={activeAutosaveError}
                      onRetry={flushActiveAutosave}
                    />
                  ) : null}
                  {activeMode === 'lore' && !isOverview && !isCreatorActive && !isOpeningPresetActive && draft && (
                    <Button className={iconActionClassName} variant="outline" size="icon" disabled={saving} onClick={handleDelete} aria-label={t('settingPanel.deleteLore')}>
                      <Trash2 data-icon="inline-start" />
                    </Button>
                  )}
                  {isIndex && <div ref={setIndexHeaderActionsTarget} className="contents" />}
                  <ConfigManagerToggle
                    open={agentOpen}
                    label={t('settingPanel.loreAgent.title')}
                    onToggle={() => setAgentOpen((open) => !open)}
                  />
                </>
              )}
              className="bg-[var(--nova-surface-2)] text-[var(--nova-text)]"
              topbarClassName="min-h-12"
            >
              {activeMode === 'lore' ? (
                <>
                  {!loading && !loadError && <div className={isOverview ? 'h-full min-h-0' : 'hidden'}>
                    <LoreLibrary
                      key={projectId}
                      projectId={projectId}
                      items={items}
                      filteredItems={filteredItems}
                      filters={filters}
                      onFiltersChange={setFilters}
                      onOrganizeTypes={organizeLoreTypes}
                      organizingDisabled={saving}
                      query={query}
                      onQueryChange={setQuery}
                      onSelect={handleSelectLore}
                      onCreate={handleCreateLore}
                      onChanged={handleLibraryItemChanged}
                      onReload={refreshItems}
                      onGenerate={generateLibraryImages}
                    />
                  </div>}

                  {loading ? (
                    <LoadingState label={t('common.loading')} className="h-full min-h-0" />
                  ) : creating ? (
                    <LoreCreateEditor
                      key={`${projectId}:${creating.id}`}
                      projectId={projectId}
                      category={creating.createType}
                      categoryLabel={creating.name || t(creating.labelKey)}
                      importance={creating.createType === 'character' ? 'major' : 'important'}
                      loadMode={creating.createType === 'character' ? 'resident' : 'auto'}
                      items={items}
                      onCancel={() => { setCreating(null); setActiveId(LORE_OVERVIEW_ID) }}
                      onCreated={(item) => {
                        setCreating(null)
                        setCreatedId(item.id)
                        setItems((current) => [...current.filter((entry) => entry.id !== item.id), item])
                        setActiveId(item.id)
                        notifyLoreUpdated({ projectId, ids: [item.id] })
                      }}
                    />
                  ) : isIndex ? (
                    <LoreIndexDocument key={projectId} projectId={projectId} items={items} headerActionsTarget={indexHeaderActionsTarget} onSelect={id => void handleSelectLore(id)} onChanged={item => setItems(current => [...current.filter(i => i.id !== item.id), item])} onFlushHandlerChange={handleIndexFlush} />
                  ) : isOverview ? null : items.length === 0 && !loadError && !activeId ? (
                    <EmptyState
                      icon={Database}
                      title={t('settingPanel.lore.emptyTitle')}
                      description={t('settingPanel.lore.emptyDescription')}
                      action={{ label: t('settingPanel.lore.emptyAction'), onClick: () => void handleCreateLore() }}
                      variant="page"
                    />
                  ) : activeId === CREATOR_ENTRY_ID ? (
                    <CreatorEditor content={creatorContent} setContent={setCreatorContent} onSave={flushActiveAutosave} />
                  ) : activeId === INTERACTIVE_OPENING_PRESET_ENTRY_ID ? (
                    <OpeningPresetEditor presets={openingPresets} activeId={activeOpeningPresetId} setActiveId={setActiveOpeningPresetId} setPresets={setOpeningPresets} onSave={flushActiveAutosave} />
                  ) : (
                    <LoreEditor
                      autoFocusContent={createdId === draft?.id}
                      projectId={projectId}
                      onInspectMaterial={(material) => {
                        setPendingLoreImageTask({ key: `lore-material-${Date.now()}`, instruction: `Read the selected image using the read tool and describe it as a creative reference. Do not modify lore. Selected material: ${JSON.stringify({ item_id: draft?.id, material_id: material.id, path: material.path, name: material.name, description: material.description })}` })
                        setAgentOpen(true)
                      }}
                      draft={draft}
                      items={items}
                      tagDraft={tagDraft}
                      residentTotalBytes={residentLoreBytes}
                      searchQuery={query}
                      setDraft={setDraft}
                      setTagDraft={setTagDraft}
                      onSave={flushActiveAutosave}
                      onSelectItem={handleSelectLore}
                      documentReview={documentReview}
                      documentReviewNavigationIntent={documentReviewNavigationIntent}
                      onPrepareReviewSnapshot={prepareLoreReviewSnapshot}
                    />
                  )}
                </>
              ) : (
                <CreatorEditor content={creatorContent} setContent={setCreatorContent} onSave={flushActiveAutosave} />
              )}
            </FeaturePageShell>
          </main>
        )}
      </ResourceWorkspace>
      <LoreClassificationDialog
        open={loreClassificationOpen}
        projectId={projectId}
        onOpenChange={setLoreClassificationOpen}
        onApplied={(nextItems) => {
          setItems(nextItems)
          const selectedItem = nextItems.find((item) => item.id === activeId)
          if (selectedItem) mergeSavedLoreItem(selectedItem)
          notifyLoreUpdated({ projectId, ids: selectedItem ? [selectedItem.id] : [] })
        }}
      />
      <ConfirmDialog
        open={Boolean(deleteLoreTarget)}
        onOpenChange={(open) => {
          if (!open && !saving) setDeleteLoreTarget(null)
        }}
        title={t('settingPanel.deleteLore')}
        description={t('settingPanel.confirmDeleteLore', { name: deleteLoreTarget?.name || '' })}
        confirmLabel={t('common.delete')}
        tone="danger"
        onConfirm={confirmDeleteLoreTarget}
      />
    </section>
  )
}

function notifyOpeningPresetUpdated() {
  if (typeof window === 'undefined') return
  window.dispatchEvent(new CustomEvent(INTERACTIVE_OPENING_PRESET_UPDATED_EVENT))
}

function ModeIcon({ mode }: { mode: SettingPanelMode }) {
  if (mode === 'creator') return <BookMarked className="h-3.5 w-3.5 shrink-0 text-[var(--nova-text-muted)]" />
  if (mode === 'teller') return <SlidersHorizontal className="h-3.5 w-3.5 shrink-0 text-[var(--nova-text-muted)]" />
  return <Database className="h-3.5 w-3.5 shrink-0 text-[var(--nova-text-muted)]" />
}

function loreItemToDirectoryItem(item: LoreItem, projectId: string, t: (key: string) => string): ResourceDirectoryItem {
  const imageSrc = loreImageURL(projectId, item)
  const badges: ResourceDirectoryBadge[] = [{
    label: item.load_mode === 'resident' ? t('settingPanel.lore.loadModeBadge.resident') : t('settingPanel.lore.loadModeBadge.onDemand'),
    title: loreLoadModeLabel(item.load_mode, t),
    tone: item.load_mode === 'resident' ? 'default' : 'outline',
  }]
  if (item.type === 'character' && hasLoreProtagonistTag(item.tags || [])) {
    badges.unshift({ label: t('loreWorkspace.protagonistTag'), tone: 'warning' })
  }
  if (item.enabled === false) {
    badges.push({ label: t('settingPanel.disabled'), tone: 'muted' })
  }
  return {
    id: item.id,
    title: item.name,
    thumbnailUrl: imageSrc || null,
    badges,
    disabled: item.enabled === false,
  }
}

function panelTitle(mode: SettingPanelMode, t: (key: string) => string) {
  if (mode === 'creator') return t('settingPanel.mode.creator')
  if (mode === 'teller') return t('settingPanel.mode.teller')
  return t('settingPanel.mode.lore')
}

function editorTitle(mode: Exclude<SettingPanelMode, 'teller'>, draft: LoreItem | null, t: (key: string) => string) {
  if (mode === 'creator') return CREATOR_PATH
  return draft?.name || t('settingPanel.mode.lore')
}

function editorSubtitle(draft: LoreItem | null, t: (key: string) => string, categoryName?: string) {
  if (!draft) return t('settingPanel.editor.loreSubtitle')
  return `${draft.enabled === false ? t('settingPanel.disabled') : t('settingPanel.enabled')} · ${categoryName || loreTypeLabel(draft.type, t)} · ${loreImportanceLabel(draft.importance, t)} · ${loreLoadModeLabel(draft.load_mode, t)} · ${(draft.tags || []).join('，') || t('settingPanel.editor.noTags')}`
}
