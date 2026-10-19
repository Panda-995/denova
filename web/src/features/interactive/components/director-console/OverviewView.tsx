import { useMemo } from 'react'
import { Activity } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { InlineErrorNotice } from '@/components/common/inline-error-notice'
import type { LoreItem } from '@/lib/api-client/types'
import type { BranchPlan, Snapshot, StoryProtagonist } from '../../types'
import { ChangesSummary } from '../story-state/ChangesSummary'
import { buildStoryStateModel, type ActorStateEntry } from '../story-state/model'
import { BranchPlanSummary } from './BranchPlanView'
import { StateDetailsDialog } from '../story-state/StateDetailsDialog'
import { ActorReferenceProvider } from '../story-state/actor-reference'

interface OverviewViewProps {
  projectId?: string
  loreItems?: LoreItem[]
  protagonist?: StoryProtagonist
  snapshot: Snapshot | null
  stateError?: string
  plan?: BranchPlan
  planningEnabled: boolean
  branchPlanEditingDisabled?: boolean
  onBranchPlanUpdate?: (markdown: string, baseRevision: string) => void | Promise<void>
}

export function OverviewView({ projectId, loreItems, protagonist, snapshot, stateError, plan, planningEnabled, branchPlanEditingDisabled = false, onBranchPlanUpdate }: OverviewViewProps) {
  const { t } = useTranslation()
  const model = useMemo(() => buildStoryStateModel(snapshot), [snapshot])
  const actors = useMemo<ActorStateEntry[]>(() => [
    ...model.actors,
    ...model.archivedActors.map((entry): ActorStateEntry => [entry.actorId, { name: entry.name, template_id: entry.templateId }]),
  ], [model.actors, model.archivedActors])
  const error = snapshot?.current_turn?.state_error || stateError

  return (
    <ActorReferenceProvider actors={actors}>
      <div className="director-console__scroll h-full min-h-0 overflow-y-auto px-3 py-3">
        <div className="flex flex-col gap-3">
          <BranchPlanSummary
            plan={plan}
            planningEnabled={planningEnabled}
            editingDisabled={branchPlanEditingDisabled}
            onUpdate={onBranchPlanUpdate}
          />
          {error ? <InlineErrorNotice message={error} /> : null}
          <section className="story-state-ledger overflow-hidden rounded-xl border border-[var(--nova-border)] bg-[var(--director-panel)]">
            {model.changes.length > 0 ? (
              <ChangesSummary changes={model.changes} actors={actors} schema={snapshot?.actor_state_schema} standalone />
            ) : (
              <div className="flex items-center gap-2 px-3 py-2.5 text-[10px] text-[var(--nova-text-faint)]">
                <Activity className="size-3.5" />
                <span>{t('directorPanel.stateDeltaEmpty')}</span>
              </div>
            )}
          </section>
          <StateDetailsDialog projectId={projectId} loreItems={loreItems} protagonist={protagonist} snapshot={snapshot} stateError={stateError} />
        </div>
      </div>
    </ActorReferenceProvider>
  )
}
