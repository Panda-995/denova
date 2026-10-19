import { useMemo, useState, type ReactElement } from 'react'
import { ArrowUpRight, Gauge } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { InlineErrorNotice } from '@/components/common/inline-error-notice'
import { Button } from '@/components/ui/button'
import { useToolNavigation } from '@/components/Chat/tool-navigation'
import type { LoreItem } from '@/lib/api-client/types'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle, DialogTrigger } from '@/components/ui/dialog'
import type { Snapshot, StoryProtagonist } from '../../types'
import { buildStoryStateModel } from './model'
import { StoryStateDetails } from './StoryStateLedger'

interface StateDetailsDialogProps {
  projectId?: string
  loreItems?: LoreItem[]
  protagonist?: StoryProtagonist
  snapshot: Snapshot | null
  stateError?: string
  /** A custom button for opening the shared state view from another surface. */
  trigger?: ReactElement
}

export function StateDetailsDialog({ projectId, loreItems = [], protagonist, snapshot, stateError, trigger }: StateDetailsDialogProps) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const navigation = useToolNavigation()
  const model = useMemo(() => buildStoryStateModel(snapshot), [snapshot])
  const actorCount = model.actors.length
  const worldCount = model.worldFacts.length
  const summary = t('directorPanel.overview.state.summary', { actors: actorCount, world: worldCount })
  const error = snapshot?.current_turn?.state_error || stateError

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        {trigger ?? <Button type="button" variant="outline" className="h-auto w-full justify-start gap-3 px-3 py-3 text-left whitespace-normal">
          <Gauge data-icon="inline-start" />
          <span className="min-w-0 flex-1">
            <span className="block text-xs font-semibold">{t('directorPanel.overview.state.title')}</span>
            <span className="mt-0.5 block truncate text-[10px] font-normal text-muted-foreground">{summary}</span>
          </span>
          <ArrowUpRight data-icon="inline-end" />
        </Button>}
      </DialogTrigger>
      <DialogContent className="director-console grid max-h-[85dvh] w-[calc(100vw-2rem)] max-w-6xl grid-rows-[auto_minmax(0,1fr)] gap-0 overflow-hidden bg-[var(--director-canvas)] p-0 text-[var(--nova-text)] max-sm:w-[calc(100vw-1rem)] max-md:max-h-[85dvh] max-md:overflow-hidden">
        <DialogHeader className="border-b border-[var(--nova-border)] bg-[var(--director-panel)] px-4 py-3 pr-12 text-left">
          <div className="flex min-w-0 items-baseline gap-2">
            <DialogTitle className="text-sm">{t('directorPanel.overview.state.dialogTitle')}</DialogTitle>
            <span className="truncate text-[11px] text-[var(--nova-text-faint)]">{t('storyStage.state.changesTitle', { count: model.changes.length })}</span>
          </div>
          <DialogDescription className="text-[11px] text-[var(--nova-text-faint)]">
            {t('directorPanel.overview.state.dialogDescription', { summary })}
          </DialogDescription>
        </DialogHeader>
        <div className="flex min-h-0 flex-col p-3">
          {error ? <InlineErrorNotice className="mb-3 shrink-0" message={error} /> : null}
          <StoryStateDetails snapshot={snapshot} actorLore={projectId ? {
            projectId,
            items: loreItems,
            protagonist,
            onOpenItem: navigation ? (id) => {
              setOpen(false)
              navigation.open({ kind: 'lore_item', id })
            } : undefined,
          } : undefined} />
        </div>
      </DialogContent>
    </Dialog>
  )
}
