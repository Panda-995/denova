import type {
  BranchSummary,
  GamePlanningTemplate,
  ImagePreset,
  InteractiveStoryUpdateInput,
  Snapshot,
  StorySummary,
  PresentationMaterial,
  Teller,
} from '../types'
import { DirectorConsole } from './director-console/DirectorConsole'
import type { LoreItem } from '@/lib/api-client/types'
import { DEFAULT_STORY_STATE_DISPLAY, type StoryStateDisplayPreference } from './story-state/display-preference'

interface DirectorPanelProps {
  projectId?: string
  storyId?: string
  story?: StorySummary
  loreItems?: LoreItem[]
  planningTemplates?: GamePlanningTemplate[]
  tellers?: Teller[]
  imagePresets?: ImagePreset[]
  onPlanningTemplateChange?: (templateId: string) => void | Promise<void>
  onStoryUpdate?: (input: InteractiveStoryUpdateInput) => void | Promise<void>
  onOpenPresets?: () => void
  branchId?: string
  branches: BranchSummary[]
  snapshot: Snapshot | null
  branchPlanEditingDisabled?: boolean
  onBackgroundChange?: (turnId: string, background?: PresentationMaterial) => Promise<void>
  onBranchPlanUpdate?: (markdown: string, baseRevision: string) => void | Promise<void>
  stateDisplayPreference?: StoryStateDisplayPreference
  onStateDisplayPreferenceChange?: (value: StoryStateDisplayPreference) => void
  onSwitchBranch: (branchId: string) => void | Promise<void>
  onOpenBranchTimeline: () => void
}

export function DirectorPanel({
  projectId,
  storyId,
  story,
  loreItems,
  planningTemplates = [],
  tellers = [],
  imagePresets = [],
  onPlanningTemplateChange,
  onStoryUpdate,
  onOpenPresets,
  branchId,
  branches,
  snapshot,
  branchPlanEditingDisabled = false,
  onBackgroundChange,
  onBranchPlanUpdate,
  stateDisplayPreference = DEFAULT_STORY_STATE_DISPLAY,
  onStateDisplayPreferenceChange = noopStateDisplayPreferenceChange,
  onSwitchBranch,
  onOpenBranchTimeline,
}: DirectorPanelProps) {
  return (
    <DirectorConsole
      projectId={projectId}
      storyId={storyId}
      story={story}
      loreItems={loreItems}
      planningTemplates={planningTemplates}
      tellers={tellers}
      imagePresets={imagePresets}
      onPlanningTemplateChange={onPlanningTemplateChange}
      onStoryUpdate={onStoryUpdate}
      onOpenPresets={onOpenPresets}
      branchId={branchId || snapshot?.branch_id || ''}
      branches={branches}
      snapshot={snapshot}
      branchPlanEditingDisabled={branchPlanEditingDisabled}
      onBackgroundChange={onBackgroundChange}
      onBranchPlanUpdate={onBranchPlanUpdate}
      stateError={snapshot?.current_turn?.state_error || ''}
      stateDisplayPreference={stateDisplayPreference}
      onStateDisplayPreferenceChange={onStateDisplayPreferenceChange}
      onSwitchBranch={onSwitchBranch}
      onOpenBranchTimeline={onOpenBranchTimeline}
    />
  )
}

function noopStateDisplayPreferenceChange(_value: StoryStateDisplayPreference) {}
