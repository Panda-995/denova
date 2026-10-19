import { ChevronDown, Play, SlidersHorizontal } from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useIsMobile } from '@/hooks/useIsMobile'
import { Button } from '@/components/ui/button'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import { Field, FieldDescription, FieldLabel } from '@/components/ui/field'
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Spinner } from '@/components/ui/spinner'
import type { LoreItem } from '@/lib/api'
import { cn } from '@/lib/utils'
import { hasLoreProtagonistTag } from '@/features/lore/tags'
import type { ConversationConfigChanges, ConversationConfigController, ConversationConfigSnapshot } from '@/features/conversation-config/types'
import { normalizeThinkingLevel } from '@/features/settings/thinking-levels'
import { normalizeStoryCheckSettings } from '../check-settings'
import { gamePlanningTemplateName } from '../game-planning'
import { normalizeStoryImageSettings } from '../image-settings'
import { DEFAULT_NARRATIVE_STYLE_ID } from '../narrative-style'
import { DEFAULT_INTERACTIVE_CHOICE_COUNT, DEFAULT_INTERACTIVE_REPLY_TARGET_CHARS, truncateStoryOpeningText, type BookOpeningPreset, type StoryCreateInput } from '../opening'
import type { GamePlanningTemplate, ImagePreset, StoryOpeningConfig, StoryPresentationSettings, StoryProtagonist, StorySummary, Teller } from '../types'
import { StoryPresentationControls } from './director-console/StoryPresentationControls'
import { StoryOpeningSelector } from './story-setup/StoryOpeningSelector'
import { StoryProtagonistSelector } from './story-setup/StoryProtagonistSelector'
import { StorySetupAdvanced, type StorySetupSettings } from './story-setup/StorySetupAdvanced'
import { ConversationConfigError } from '@/features/conversation-config/ConversationConfigError'
import { refreshProjectSettings } from '@/features/settings/api'
import { gameDefaultPresentation, withGameDefaultModules, type GameCreationDefaults, type GameCreationPreferences } from '../game-creation-defaults'
import { GameDefaultsDialog } from './GameDefaultsDialog'

interface NewStorySetupPanelProps {
  projectId: string
  tellers: Teller[]
  planningTemplates: GamePlanningTemplate[]
  imagePresets: ImagePreset[]
  loreItems?: LoreItem[]
  bookOpeningPresets?: BookOpeningPreset[]
  conversationConfig: ConversationConfigController
  story?: StorySummary
  onRequestLoreInit?: () => void
  onOpenPresets?: () => void
  onCancel: () => void
  onCreate: (input: StoryCreateInput) => void | Promise<void>
}

export function NewStorySetupPanel({
  projectId,
  tellers,
  planningTemplates,
  imagePresets,
  loreItems = [],
  bookOpeningPresets = [],
  conversationConfig,
  story,
  onRequestLoreInit,
  onOpenPresets,
  onCancel,
  onCreate,
}: NewStorySetupPanelProps) {
  const { t } = useTranslation()
  const isMobile = useIsMobile()
  const initialProtagonist = story?.protagonist || defaultStoryProtagonist(loreItems)
  const [planningTemplateId, setPlanningTemplateId] = useState(story?.planning_template_id || 'default')
  const [protagonist, setProtagonist] = useState<StoryProtagonist>(initialProtagonist)
  const [rememberedProtagonist, setRememberedProtagonist] = useState<StoryProtagonist>()
  const [opening, setOpening] = useState<StoryOpeningConfig>(() => story?.opening || { mode: 'custom' })
  const [settings, setSettings] = useState<StorySetupSettings>(() => initialSettings(story, conversationConfig.snapshot))
  const [advancedOpen, setAdvancedOpen] = useState(!isMobile)
  const [presentationSettings, setPresentationSettings] = useState<StoryPresentationSettings>(() => ({ background: true, characters: true, ...story?.presentation_settings }))
  const [creating, setCreating] = useState(false)
  const [error, setError] = useState('')
  const [defaultsLoaded, setDefaultsLoaded] = useState(story ? projectId : '')
  const [defaultsError, setDefaultsError] = useState(false)
  const [defaultsReload, setDefaultsReload] = useState(0)
  const [prefillSource, setPrefillSource] = useState<'remembered' | 'prefilled' | 'newStoryHelp' | 'resetApplied'>('newStoryHelp')
  const [resetting, setResetting] = useState(false)
  const [saveDefaultsOpen, setSaveDefaultsOpen] = useState(false)
  const initialProtagonistRef = useRef<StoryProtagonist>(initialProtagonist)
  const protagonistSelectionTouchedRef = useRef(false)
  const defaultsLoading = resetting || (!story && defaultsLoaded !== projectId)
  const planningTemplate = planningTemplates.find((item) => item.id === planningTemplateId)
  const planningTemplateName = planningTemplate ? gamePlanningTemplateName(planningTemplate, t) : planningTemplateId
  const advancedSummary = useMemo(() => t('storyPicker.setup.advanced.summary', {
    planning: settings.planningEnabled ? t('storyPicker.setup.advanced.planningOn') : t('storyPicker.setup.advanced.planningOff'),
    checks: settings.moduleRefs.rule_system_disabled ? t('storyPicker.setup.advanced.checksOff') : t('storyPicker.setup.advanced.checksOn'),
    images: settings.imageSettings.mode === 'interval' ? t('storyPicker.setup.advanced.imagesOn') : t('storyPicker.setup.advanced.imagesOff'),
  }), [settings.imageSettings.mode, settings.moduleRefs.rule_system_disabled, settings.planningEnabled, t])
  // Keep the loaded form stable when remembered settings refresh after a save.
  const runtimeConfigLoading = (conversationConfig.loading && !conversationConfig.initialized)
    || (!conversationConfig.error && !settings.modelProfileId)
  const runtimeConfigPending = conversationConfig.loading || conversationConfig.saving
  const runtimeConfigReady = conversationConfig.initialized && Boolean(settings.modelProfileId)
  const startButtonLoading = creating || runtimeConfigLoading || defaultsLoading
  let startButtonLabel = t('storyPicker.setup.start')
  if (runtimeConfigLoading || defaultsLoading) startButtonLabel = t('common.loading')
  if (creating) startButtonLabel = t('storyPicker.setup.starting')

  useEffect(() => {
    if (conversationConfig.error) setAdvancedOpen(true)
  }, [conversationConfig.error])

  const applyCreationPreferences = (defaults?: GameCreationDefaults, previous?: GameCreationPreferences) => {
    const resources = previous?.resources || defaults || {}
    setSettings(current => {
      const base = initialSettings(undefined, null)
      const moduleRefs = withGameDefaultModules(base.moduleRefs, resources)
      for (const flag of previous?.disabled_modules || []) moduleRefs[flag] = true
      return {
        ...base,
        // Loading/resetting story preferences must not write user model choices.
        modelProfileId: current.modelProfileId, thinkingLevel: current.thinkingLevel,
        moduleRefs,
        imageSettings: normalizeStoryImageSettings(previous
          ? { mode: previous.image_mode, interval_turns: previous.image_interval_turns, preset_id: previous.image_preset_id }
          : { ...base.imageSettings, preset_id: resources.image_preset_id || base.imageSettings.preset_id }),
        stateSchemaMode: previous?.state_schema_mode || (resources.actor_state_id === '' ? 'generate' : base.stateSchemaMode),
        ...(previous ? {
          customAgentId: previous.custom_agent_id,
          planningEnabled: previous.planning_enabled,
          replyTargetChars: previous.reply_target_chars,
          choiceCount: previous.choice_count,
          checkSettings: normalizeStoryCheckSettings({
            difficulty_shift: previous.difficulty_shift, roll_modifier: previous.roll_modifier,
            rule_state_consumption_mode: previous.rule_state_consumption_mode, rule_visibility_mode: previous.rule_visibility_mode,
          }),
        } : {}),
      }
    })
    setPlanningTemplateId(resources.planning_template_id || 'default')
    const presentation = gameDefaultPresentation(resources, loreItems, t('gameDefaults.unavailable'))
    if (presentation.default_background && previous?.background_focus) {
      presentation.default_background.focus = previous.background_focus
    }
    setPresentationSettings({
      ...presentation,
      ...(previous ? { background: previous.background, characters: previous.characters } : {}),
    })
  }

  useEffect(() => {
    if (story || !projectId) return
    let cancelled = false
    setDefaultsError(false)
    void refreshProjectSettings(projectId).then(snapshot => {
      if (cancelled) return
      const defaults = snapshot.workspace?.game_creation_defaults
      const previous = snapshot.workspace?.game_last_creation
      setRememberedProtagonist(previous?.protagonist)
      applyCreationPreferences(defaults, previous)
      setPrefillSource(previous ? 'remembered' : defaults && Object.keys(defaults).length ? 'prefilled' : 'newStoryHelp')
      setDefaultsLoaded(projectId)
    }).catch(reason => {
      console.error('[story-setup] failed to load book defaults', reason)
      if (!cancelled) setDefaultsError(true)
    })
    return () => { cancelled = true }
    // Seed this form once. Catalog refreshes and later settings saves must not
    // replace choices the user has already made in the open form.
  }, [projectId, story?.id, defaultsReload])

  const resetDefaults = async () => {
    setResetting(true)
    setError('')
    try {
      // Defaults may have been saved/imported since this form opened. Reset is
      // local to the form and never clears the durable last-creation snapshot.
      const snapshot = await refreshProjectSettings(projectId)
      applyCreationPreferences(snapshot.workspace?.game_creation_defaults)
      setPrefillSource('resetApplied')
      console.info('[story-setup] restored creation defaults', { projectId })
    } catch (reason) {
      console.error('[story-setup] failed to restore creation defaults', reason)
      setError(t('gameDefaults.loadFailed'))
    } finally {
      setResetting(false)
    }
  }

  useEffect(() => {
    setPresentationSettings(current => {
      const bg = current.default_background
      if (!bg || bg.path) return current
      const item = loreItems.find(item => item.id === bg.item_id && item.enabled)
      const asset = item?.resolved_materials?.find(asset => asset.id === bg.asset_id)
      return asset ? { ...current, default_background: { ...bg, name: asset.name, path: asset.path || '' } } : current
    })
  }, [loreItems])

  const formDefaults = useMemo<GameCreationDefaults>(() => ({
    narrative_style_id: settings.moduleRefs.narrative_style_disabled ? '' : settings.moduleRefs.narrative_style_id,
    image_preset_id: settings.moduleRefs.image_preset_disabled ? '' : settings.moduleRefs.image_preset_id,
    actor_state_id: settings.stateSchemaMode === 'generate' ? '' : settings.moduleRefs.actor_state_id,
    rule_system_id: settings.moduleRefs.rule_system_disabled ? '' : settings.moduleRefs.rule_system_id,
    event_package_ids: settings.moduleRefs.event_packages_disabled ? [] : settings.moduleRefs.event_package_ids,
    planning_template_id: planningTemplateId,
    default_background: presentationSettings.default_background
      ? { mode: 'image', item_id: presentationSettings.default_background.item_id, asset_id: presentationSettings.default_background.asset_id }
      : { mode: 'none' },
  }), [settings.moduleRefs, settings.stateSchemaMode, planningTemplateId, presentationSettings.default_background])

  useEffect(() => {
    const snapshot = conversationConfig.snapshot
    if (!snapshot) return
    setSettings((current) => ({
      ...current,
      modelProfileId: snapshot.profile_id || 'default',
      thinkingLevel: normalizeThinkingLevel(snapshot.thinking_level) || 'default',
    }))
  }, [conversationConfig.snapshot])

  useEffect(() => {
    // Late preferences and Lore loads may seed the draft until the user chooses.
    if (protagonistSelectionTouchedRef.current || (story && protagonist.mode !== 'default')) return
    const selected = defaultStoryProtagonist(loreItems, rememberedProtagonist)
    setProtagonist(current => sameProtagonist(current, selected) ? current : selected)
  }, [loreItems, rememberedProtagonist, protagonist.mode, story])

  const changeProtagonist = (next: StoryProtagonist) => {
    protagonistSelectionTouchedRef.current = true
    setProtagonist(next)
  }

  const changeSettings = async (next: StorySetupSettings) => {
    const changes: ConversationConfigChanges = {}
    if (next.modelProfileId !== settings.modelProfileId) changes.profile_id = next.modelProfileId
    if (next.thinkingLevel !== settings.thinkingLevel) changes.thinking_level = next.thinkingLevel
    if (Object.keys(changes).length > 0) {
      setError('')
      if (!await conversationConfig.patch(changes)) {
        setError(t('storyPicker.setup.model.saveFailed'))
        return
      }
      // Other opening controls may change while persistence is in progress.
      setSettings((current) => ({ ...current, modelProfileId: next.modelProfileId, thinkingLevel: next.thinkingLevel }))
      return
    }
    setSettings(next)
  }

  const submit = async () => {
    if (creating || defaultsLoading || defaultsError) return
    setError('')
    if (!conversationConfig.initialized || !settings.modelProfileId) {
      setError(t('storyPicker.setup.model.loadFailed'))
      return
    }
    const validationError = validateDraft(protagonist, opening, loreItems, t)
    if (validationError) {
      setError(validationError)
      return
    }
    setCreating(true)
    try {
      // Preserve the selected ID. A missing resource must be repaired explicitly,
      // rather than silently substituting the user's recent narrative style.
      const tellerID = settings.moduleRefs.narrative_style_disabled ? '' : settings.moduleRefs.narrative_style_id || DEFAULT_NARRATIVE_STYLE_ID
      const moduleRefs = {
        ...settings.moduleRefs,
        actor_state_id: settings.moduleRefs.actor_state_id || 'default',
        actor_state_disabled: settings.stateSchemaMode === 'generate',
      }
      const protagonistInput = protagonistForSubmit(protagonist)
      const includeProtagonist = !story || !sameProtagonist(protagonist, initialProtagonistRef.current)
      await onCreate({
        title: story?.title_source === 'pending' ? '' : story?.title || '',
        ...(!story ? { custom_agent_id: settings.customAgentId } : {}),
        profile_id: settings.modelProfileId,
        thinking_level: settings.thinkingLevel,
        origin: story?.origin || '',
        ...(includeProtagonist ? { protagonist: protagonistInput } : {}),
        story_teller_id: tellerID,
        planning_template_id: planningTemplateId,
        planning_mode: settings.planningEnabled ? 'enabled' : 'disabled',
        module_refs: moduleRefs,
        reply_target_chars: settings.replyTargetChars,
        choice_count: settings.choiceCount,
        opening: openingForSubmit(opening),
        image_settings: { ...settings.imageSettings, preset_id: settings.imageSettings.preset_id || moduleRefs.image_preset_id || 'game-cg' },
        check_settings: settings.checkSettings,
        presentation_settings: presentationSettings,
        state_schema_policy: { mode: settings.stateSchemaMode },
      })
    } catch (reason) {
      console.error('[story-setup] Failed to save and start story', reason)
      setError(reason instanceof Error ? reason.message : t('storyPicker.createFailed'))
      setCreating(false)
    }
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col bg-[var(--nova-surface-2)]">
      <div className="min-h-0 flex-1 overflow-y-auto px-4 pb-6 pt-5 sm:px-7 lg:px-10">
        <section className="mx-auto w-full max-w-5xl" aria-labelledby="new-story-title">
          <header className="mb-4">
            <h2 id="new-story-title" className="text-xl font-semibold tracking-[-0.02em] text-foreground sm:text-2xl">{story ? t('storyPicker.setup.resumeTitle') : t('storyPicker.setup.title')}</h2>
            <p className="mt-1 text-sm text-muted-foreground">{t('storyPicker.setup.description')}</p>
          </header>

          {defaultsError && <div role="alert" className="mb-3 text-sm text-destructive">{t('gameDefaults.loadFailed')} <Button variant="outline" onClick={() => setDefaultsReload(value => value + 1)}>{t('common.retry')}</Button></div>}
          <fieldset disabled={defaultsLoading || creating} className="flex min-w-0 flex-col gap-4">
            <ConversationConfigError controller={conversationConfig} agentKey="interactive_story" />
            <StoryProtagonistSelector projectId={projectId} value={protagonist} loreItems={loreItems} onChange={changeProtagonist} onRequestLoreInit={onRequestLoreInit} />
            <StoryOpeningSelector value={opening} presets={bookOpeningPresets} onChange={setOpening} />

            <Collapsible open={advancedOpen} onOpenChange={setAdvancedOpen} className="overflow-hidden rounded-xl border border-border bg-card">
              <CollapsibleTrigger asChild>
                <button type="button" className="flex w-full items-center gap-3 px-4 py-3 text-left transition-colors hover:bg-muted/50 sm:px-5" aria-label={t('storyPicker.setup.advanced.title')}>
                  <span className="flex size-8 shrink-0 items-center justify-center rounded-lg border border-border bg-background text-primary"><SlidersHorizontal className="size-4" /></span>
                  <span className="min-w-0 flex-1">
                    <span className="block text-sm font-medium text-foreground">{t('storyPicker.setup.advanced.title')}</span>
                    <span className="mt-0.5 block truncate text-xs text-muted-foreground">{advancedSummary}</span>
                  </span>
                  <ChevronDown className={cn('size-4 shrink-0 text-muted-foreground transition-transform', advancedOpen && 'rotate-180')} />
                </button>
              </CollapsibleTrigger>
              <CollapsibleContent className="border-t border-border bg-muted/20 p-3 sm:p-4">
                {!story && <div className="mb-4 flex min-w-0 flex-col gap-2" data-testid="story-creation-preferences">
                  <p className="text-xs text-muted-foreground">{t(`gameDefaults.${prefillSource}`)}</p>
                  <div className="flex flex-wrap gap-2">
                    <Button type="button" variant="outline" size="sm" className="h-auto min-h-7 max-w-full whitespace-normal" disabled={defaultsLoading || defaultsError || creating} onClick={() => setSaveDefaultsOpen(true)}>{t('gameDefaults.saveSelection')}</Button>
                    <Button type="button" variant="ghost" size="sm" disabled={defaultsLoading || defaultsError || creating || conversationConfig.saving} onClick={() => void resetDefaults()}>{t('gameDefaults.reset')}</Button>
                  </div>
                </div>}
                <StorySetupAdvanced
                  planningControl={
                    <Field className="rounded-lg border border-border bg-background p-3">
                      <FieldLabel htmlFor="story-setup-planning-template">{t('storyPicker.gamePlanning')}</FieldLabel>
                      <Select value={planningTemplateId} onValueChange={setPlanningTemplateId}>
                        <SelectTrigger id="story-setup-planning-template" className="w-full bg-background">
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent position="popper">
                          <SelectGroup>
                            {!planningTemplate && <SelectItem value={planningTemplateId}>{t('gameDefaults.resourceUnavailable', { id: planningTemplateId })}</SelectItem>}
                            {planningTemplates.map((item) => (
                              <SelectItem key={item.id} value={item.id}>
                                {gamePlanningTemplateName(item, t)}
                              </SelectItem>
                            ))}
                          </SelectGroup>
                        </SelectContent>
                      </Select>
                      <FieldDescription className="text-xs">
                        {t('storyPicker.setup.planningTemplateHint', { template: planningTemplateName })}
                      </FieldDescription>
                    </Field>
                  }
                  presentationControl={<StoryPresentationControls projectId={projectId} value={presentationSettings} disabled={creating} onChange={setPresentationSettings} />}
                  projectId={projectId}
                  newStory={!story}
                  tellers={tellers}
                  imagePresets={imagePresets}
                  value={settings}
                  onChange={(next) => { void changeSettings(next) }}
                  runtimeConfigLoading={runtimeConfigLoading}
                  runtimeConfigPending={runtimeConfigPending}
                  runtimeConfigError={conversationConfig.error}
                  onRuntimeConfigReload={() => void conversationConfig.reload()}
                  onOpenPresets={onOpenPresets}
                />
              </CollapsibleContent>
            </Collapsible>
          </fieldset>

          {error ? <div role="alert" className="mt-4 rounded-lg border border-destructive/30 bg-destructive/10 px-3 py-2 text-xs text-destructive">{error}</div> : null}
        </section>
      </div>
      <footer data-testid="story-setup-footer" className="shrink-0 border-t border-border bg-[var(--nova-surface-2)] px-4 py-3 sm:px-7 lg:px-10">
        <div className="mx-auto flex w-full max-w-5xl items-center justify-end gap-2">
          {!story ? <Button type="button" variant="ghost" disabled={creating} onClick={onCancel}>{t('common.cancel')}</Button> : null}
          <Button
            type="button"
            disabled={creating || defaultsLoading || defaultsError || runtimeConfigPending || !runtimeConfigReady}
            onClick={() => void submit()}
          >
            {startButtonLoading ? <Spinner /> : <Play data-icon="inline-start" />}
            {startButtonLabel}
          </Button>
        </div>
      </footer>
      {saveDefaultsOpen && <GameDefaultsDialog defaults={formDefaults} projectID={projectId} onClose={() => setSaveDefaultsOpen(false)} />}
    </div>
  )
}

function initialSettings(
  story: StorySummary | undefined,
  runtimeConfig: ConversationConfigSnapshot | null,
): StorySetupSettings {
  const moduleRefs = {
    narrative_style_id: story?.module_refs?.narrative_style_id || story?.story_teller_id || DEFAULT_NARRATIVE_STYLE_ID,
    narrative_style_disabled: story?.module_refs?.narrative_style_disabled,
    event_package_ids: ['default'],
    rule_system_id: 'default',
    actor_state_id: 'default',
    image_preset_id: 'game-cg',
    ...(story?.module_refs || {}),
  }
  const imageSettings = normalizeStoryImageSettings(story?.image_settings || { mode: 'manual', interval_turns: 3, preset_id: moduleRefs.image_preset_id || 'game-cg' })
  return {
    customAgentId: '',
    modelProfileId: runtimeConfig ? runtimeConfig.profile_id || 'default' : '',
    thinkingLevel: normalizeThinkingLevel(runtimeConfig?.thinking_level) || 'default',
    planningEnabled: story ? story.planning_mode === 'enabled' : true,
    moduleRefs,
    replyTargetChars: story?.reply_target_chars || DEFAULT_INTERACTIVE_REPLY_TARGET_CHARS,
    choiceCount: story?.choice_count || DEFAULT_INTERACTIVE_CHOICE_COUNT,
    imageSettings,
    checkSettings: normalizeStoryCheckSettings(story?.check_settings),
    stateSchemaMode: story?.state_schema_policy?.mode || 'adapt_template',
  }
}

function validateDraft(protagonist: StoryProtagonist, opening: StoryOpeningConfig, loreItems: LoreItem[], t: ReturnType<typeof useTranslation>['t']): string {
  if (protagonist.mode === 'custom' && !protagonist.name?.trim()) return t('storyPicker.setup.protagonist.custom.nameRequired')
  if (protagonist.mode === 'lore' && !protagonist.source_lore_item_id?.trim()) return t('storyPicker.setup.protagonist.lore.required')
  if (protagonist.mode === 'default' && !loreItems.some((item) => item.enabled && item.type === 'character')) return t('storyPicker.setup.protagonist.lore.emptyRequired')
  if (opening.mode === 'preset' && !opening.preset_text?.trim()) return t('storyPicker.setup.opening.presetRequired')
  return ''
}

function defaultStoryProtagonist(loreItems: LoreItem[], remembered?: StoryProtagonist): StoryProtagonist {
  if (remembered?.mode === 'custom') return remembered
  const characters = loreItems.filter(item => item.enabled && item.type === 'character')
  const selected = characters.find(item => item.id === remembered?.source_lore_item_id)
    || characters.find(item => hasLoreProtagonistTag(item.tags))
  if (!selected) return { mode: 'default' }
  return {
    mode: 'lore',
    name: selected.name,
    profile: selected.content || selected.brief_description,
    source_lore_item_id: selected.id,
    source_lore_updated_at: selected.updated_at,
  }
}

function protagonistForSubmit(protagonist: StoryProtagonist): StoryProtagonist {
  if (protagonist.mode === 'custom') return { mode: 'custom', name: protagonist.name?.trim(), profile: protagonist.profile?.trim() }
  if (protagonist.mode === 'lore') return { mode: 'lore', source_lore_item_id: protagonist.source_lore_item_id?.trim() }
  return { mode: 'default' }
}

function openingForSubmit(opening: StoryOpeningConfig): StoryOpeningConfig {
  if (opening.mode === 'preset') return { mode: 'preset', preset_id: opening.preset_id?.trim(), preset_text: truncateStoryOpeningText(opening.preset_text || '') }
  if (opening.mode === 'custom') {
    const customText = truncateStoryOpeningText(opening.custom_text || '')
    return customText ? { mode: 'custom', custom_text: customText } : { mode: 'ai' }
  }
  return { mode: 'ai' }
}

function sameProtagonist(left: StoryProtagonist, right: StoryProtagonist): boolean {
  return JSON.stringify(protagonistForSubmit(left)) === JSON.stringify(protagonistForSubmit(right))
}
