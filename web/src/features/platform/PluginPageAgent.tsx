import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { BookMarked } from 'lucide-react'
import { WritingAgentWorkspace } from '@/features/agent-chat/WritingAgentWorkspace'
import type { AgentChatConversationState } from '@/features/agent-chat/AgentChatConversationTab'
import { buildAgentMessageViews } from '@/lib/agent-message-view'
import { getAgentChatProjects, type AgentChatProject } from '@/features/agent-chat/api'
import { WRITING_COMPOSER_SETTING_DEFAULTS, type AgentPanelProps } from '@/components/Chat/AgentPanel'
import { usePersistedUserSettings } from '@/hooks/usePersistedUserSettings'
import { InlineErrorNotice } from '@/components/common/inline-error-notice'
import { LoadingState } from '@/components/common/LoadingState'
import { Button } from '@/components/ui/button'
import type { PagePresentation } from './page-presentation'
import { platformError, type PluginPage } from './api'
import type { PluginPageLoreAgentProps } from './use-plugin-page-lore'

/** Read-only candidates from the same Project library used by the Writing dock. */
export type PluginPageLoreReferences = Pick<AgentPanelProps, 'loreReferenceLabels' | 'loreSuggestions'>

/** Plugin frames contribute context and page actions. The host owns the same
 * Project conversations, UI, settings and runtime used by the Writing dock. */
export function PluginPageAgent({ projectId, page, context, presentation, loreReferenceLabels, loreSuggestions, lore, active, onPageAction, onChange, onBusyChange }: {
  projectId: string
  page: PluginPage
  context: unknown
  presentation: PagePresentation
  active: boolean
  lore: PluginPageLoreAgentProps & { open: boolean; activeItemId: string | null }
  onPageAction: (id: string) => void
  onChange: () => void
  onBusyChange: (busy: boolean) => void
} & PluginPageLoreReferences) {
  const { t } = useTranslation()
  const project = useQuery({
    queryKey: ['plugin-page-agent-project', projectId],
    queryFn: async () => {
      const match = (await getAgentChatProjects()).find((project) => project.id === projectId)
      if (!match) throw new Error(t('platform.selectProject'))
      return match
    },
  })
  useEffect(() => () => onBusyChange(false), [onBusyChange])
  if (project.isPending) return <LoadingState label={t('common.loading')} variant="panel" layout="conversation" />
  if (!project.data || project.error) return <div className="flex flex-col gap-3 p-4">
    <InlineErrorNotice message={platformError(project.error)} />
    <Button variant="outline" onClick={() => void project.refetch()}>{t('common.retry')}</Button>
  </div>
  return <PageConversation project={project.data} page={page} context={context} presentation={presentation} active={active}
    loreReferenceLabels={loreReferenceLabels} loreSuggestions={loreSuggestions} lore={lore}
    onPageAction={onPageAction} onChange={onChange} onBusyChange={onBusyChange} />
}

function PageConversation({ project, page, context, presentation, loreReferenceLabels, loreSuggestions, lore, active, onPageAction, onChange, onBusyChange }: {
  project: AgentChatProject
} & Omit<Parameters<typeof PluginPageAgent>[0], 'projectId'>) {
  const [railVisible, setRailVisible] = useState(false)
  const { t } = useTranslation()
  const { open: loreOpen, activeItemId, ...loreAgentProps } = lore
  const settings = usePersistedUserSettings({ workspace: project.path, defaults: WRITING_COMPOSER_SETTING_DEFAULTS })
  const completedTools = useRef({ sessionId: '', keys: new Set<string>() })
  const handleConversationState = useCallback((state: AgentChatConversationState) => {
    onBusyChange(state.isStreaming)
    // Plugin tools may commit artwork during a long Agent turn. Refresh from
    // confirmed tool outcomes, including partial failures, without owning work.
    const keys = new Set(buildAgentMessageViews(state.messages)
      .filter(view => (view.kind === 'tool' || view.kind === 'tool-result') && view.toolName?.startsWith('plugin_') && view.status && view.status !== 'running')
      .map(view => `${view.key}:${view.status}`))
    const previous = completedTools.current
    completedTools.current = { sessionId: state.sessionId, keys }
    if (previous.sessionId === state.sessionId && [...keys].some(key => !previous.keys.has(key))) onChange()
  }, [onBusyChange, onChange])
  const pageInput = useMemo(() => loreOpen ? ({
    context: { plugin_id: page.pluginId, page_id: page.id, data: { surface: 'lore', itemId: activeItemId } },
    label: activeItemId ? loreReferenceLabels?.[activeItemId] || t('tab.lore') : t('tab.lore'),
    icon: BookMarked,
    onAction: onPageAction,
  }) : ({
    context: { plugin_id: page.pluginId, page_id: page.id, data: context },
    label: presentation.contextLabel,
    referenceCount: presentation.referenceCount,
    actions: presentation.actions,
    suggestions: presentation.suggestions,
    onAction: onPageAction,
  }), [context, onPageAction, page.id, page.pluginId, presentation, loreOpen, activeItemId, loreReferenceLabels, t])
  return <WritingAgentWorkspace
    {...loreAgentProps}
    active={active}
    projectId={project.id}
    projectType={project.type}
    workspace={project.path}
    activeSessionPreferenceScope={`plugin:${page.pluginId}/${page.id}`}
    composerDraftScope={`plugin:${page.pluginId}/${page.id}`}
    composerSettings={settings}
    page={pageInput}
    tellers={[]}
    selectedFile={null}
    references={[]}
    loreReferenceLabels={loreReferenceLabels}
    loreSuggestions={loreSuggestions}
    styleScenes={[]}
    textSelections={[]}
    fileSuggestions={[]}
    onReferenceRemove={noop}
    onStyleSceneRemove={noop}
    onTextSelectionRemove={noop}
    sessionRailVisible={railVisible}
    onSessionRailVisibleChange={setRailVisible}
    onSettled={onChange}
    onWorkspaceChanged={onChange}
    onConversationStateChange={handleConversationState}
  />
}
function noop() {}
