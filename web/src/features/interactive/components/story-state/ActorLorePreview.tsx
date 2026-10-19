import { BookUser, ArrowUpRight } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { ImagePreviewDialog } from '@/components/common/ImagePreviewDialog'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from '@/components/ui/card'
import { MaterialImage } from '@/features/lore/MaterialImage'
import { loreImageURL } from '@/lib/api-client/lore'
import type { LoreItem } from '@/lib/api-client/types'
import type { StoryProtagonist } from '../../types'

/** Reads current project Lore; it never merges library content into story state. */
export interface ActorLoreContext {
  projectId: string
  items: readonly LoreItem[]
  protagonist?: StoryProtagonist
  onOpenItem?: (id: string) => void
}

export function ActorLorePreview({ actorId, name, context, variant = 'details' }: {
  actorId: string
  name: string
  context: ActorLoreContext
  /** The stage uses only the cover; details surfaces also show the current Lore summary. */
  variant?: 'details' | 'cover'
}) {
  const { t } = useTranslation()
  // Protagonists already have provenance; other Actors use the library's unique
  // names. This is a current-library lookup, not a new persisted relationship.
  const id = actorId === 'protagonist' && context.protagonist?.mode === 'lore'
    ? context.protagonist.source_lore_item_id : undefined
  const nameKey = name.trim().toLowerCase()
  const item = context.items.find((entry) => entry.type === 'character' && (id
    ? entry.id === id : entry.name.trim().toLowerCase() === nameKey))
  if (!item) return null
  const image = loreImageURL(context.projectId, item)
  const cover = image ? (
    <ImagePreviewDialog src={image} title={item.name} alt={item.name}>
      <button
        type="button"
        aria-label={t('storyStage.state.lore.viewCover', { name: item.name })}
        className="block aspect-[3/4] w-full cursor-zoom-in overflow-hidden rounded-md bg-transparent outline-none focus-visible:ring-2 focus-visible:ring-ring"
      >
        <MaterialImage key={image} src={image} alt={item.name} className="h-full w-full object-cover" />
      </button>
    </ImagePreviewDialog>
  ) : null

  if (variant === 'cover') {
    if (!cover) return null
    return (
      <aside aria-label={t('storyStage.state.lore.title')} className="story-state-ledger__cover relative mx-auto w-full min-w-0 max-w-48">
        {cover}
        {context.onOpenItem ? (
          <Button
            type="button"
            variant="secondary"
            size="icon-sm"
            className="story-state-ledger__action absolute right-1.5 top-1.5 shadow-sm"
            aria-label={t('storyStage.state.lore.open')}
            title={t('storyStage.state.lore.open')}
            onClick={() => context.onOpenItem?.(item.id)}
          >
            <ArrowUpRight />
          </Button>
        ) : null}
      </aside>
    )
  }

  return (
    <aside aria-label={t('storyStage.state.lore.title')} className="min-w-0 p-3">
      <Card size="sm">
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <BookUser className="size-4 shrink-0" aria-hidden="true" />
            {t('storyStage.state.lore.title')}
          </CardTitle>
          <CardDescription>{t('storyStage.state.lore.current')}</CardDescription>
        </CardHeader>
        <CardContent className="flex min-w-0 flex-col gap-3">
          {cover ? (
            <div className="mx-auto w-full max-w-48">{cover}</div>
          ) : null}
          <p className="font-medium [overflow-wrap:anywhere]">{item.name}</p>
          <p className="whitespace-pre-wrap text-sm leading-relaxed text-muted-foreground [overflow-wrap:anywhere]">
            {item.brief_description.trim() || t('storyStage.state.lore.noDescription')}
          </p>
        </CardContent>
        {context.onOpenItem ? (
          <CardFooter>
            <Button variant="outline" size="sm" onClick={() => context.onOpenItem?.(item.id)}>
              {t('storyStage.state.lore.open')}
              <ArrowUpRight data-icon="inline-end" />
            </Button>
          </CardFooter>
        ) : null}
      </Card>
    </aside>
  )
}
