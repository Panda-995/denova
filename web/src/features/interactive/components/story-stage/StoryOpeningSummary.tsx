import { BookOpen, ChevronDown } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import type { StorySummary } from '../../types'

/** Displays the story-owned setup without adding a player action or model input. */
export function StoryOpeningSummary({ story }: { story: StorySummary }) {
  const { t } = useTranslation()
  const { opening, protagonist } = story
  const openingText = {
    ai: t('storyPicker.setup.opening.aiDescription'),
    preset: opening.preset_text || '',
    custom: opening.custom_text || '',
  } satisfies Record<StorySummary['opening']['mode'], string>

  return (
    <Collapsible defaultOpen role="region" aria-label={t('storyStage.opening.title')} className="rounded-xl border border-border bg-card text-card-foreground">
      <CollapsibleTrigger asChild>
        <Button variant="ghost" aria-label={t('storyStage.opening.title')} className="group h-auto w-full justify-start gap-2 whitespace-normal px-4 py-3 text-left">
          <BookOpen data-icon="inline-start" />
          <span className="min-w-0 flex-1">{t('storyStage.opening.title')}</span>
          <span className="text-xs text-muted-foreground">{t(`storyPicker.setup.opening.${opening.mode}`)}</span>
          <ChevronDown data-icon="inline-end" className="transition-transform group-data-[state=open]:rotate-180 motion-reduce:transition-none" />
        </Button>
      </CollapsibleTrigger>
      <CollapsibleContent>
        <dl className="flex min-w-0 flex-col gap-4 px-4 pb-4 text-sm leading-6 [overflow-wrap:anywhere]">
          <div>
            <dt className="text-xs text-muted-foreground">{t('storyStage.opening.protagonist')}</dt>
            <dd className="whitespace-pre-wrap">{protagonist.name || t('storyStage.opening.autoProtagonist')}</dd>
            {protagonist.profile && <dd className="mt-1 whitespace-pre-wrap text-muted-foreground">{protagonist.profile}</dd>}
          </div>
          {story.origin && <div>
            <dt className="text-xs text-muted-foreground">{t('storyStage.opening.premise')}</dt>
            <dd className="whitespace-pre-wrap">{story.origin}</dd>
          </div>}
          <div>
            <dt className="text-xs text-muted-foreground">{t('storyStage.opening.scene')}</dt>
            <dd className="whitespace-pre-wrap">{openingText[opening.mode]}</dd>
          </div>
        </dl>
      </CollapsibleContent>
    </Collapsible>
  )
}
