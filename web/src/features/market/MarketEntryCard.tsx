import {
  BookOpen,
  Check,
  Gamepad2,
  GitBranch,
  Package,
  Plug,
  SlidersHorizontal,
  Sparkles,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Badge } from '@/components/ui/badge'
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { cn } from '@/lib/utils'
import { localized, type MarketEntry } from './api'

export function MarketEntryCard({
  entry,
  view,
  acquired,
  onOpen,
}: {
  entry: MarketEntry
  view: string
  acquired: boolean
  onOpen: () => void
}) {
  const { t, i18n } = useTranslation()
  const Icon = entry.kinds.includes('extension.game')
    ? Gamepad2
    : entry.kinds.includes('extension.plugin')
      ? Plug
      : entry.kinds.includes('skill')
        ? Sparkles
        : entry.kinds.some((kind) => kind.startsWith('preset.'))
          ? SlidersHorizontal
          : entry.kinds.includes('lore.collection')
            ? BookOpen
            : Package
  const title = localized(entry.name, i18n.language)
  const kindLabel = t(`market.kinds.${entry.kinds[0]}`)
  return (
    <Card
      size="sm"
      data-testid="market-resource-card"
      className={cn(
        'relative h-full min-w-0 transition-shadow hover:ring-foreground/25 focus-within:ring-2 focus-within:ring-ring',
        view === 'list' && 'sm:gap-2',
      )}
    >
      <CardHeader className="flex min-w-0 flex-row items-center gap-3">
        <div className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-muted text-muted-foreground">
          <Icon className="size-5" />
        </div>
        <div className="flex min-w-0 flex-1 flex-col gap-1">
          <CardTitle className="flex min-w-0 items-center gap-2">
            <button
              className="min-w-0 truncate text-left after:absolute after:inset-0 focus:outline-none"
              title={title}
              onClick={onOpen}
            >
              {title}
            </button>
            {acquired && <Badge variant="secondary"><Check data-icon="inline-start" />{t('market.installed')}</Badge>}
          </CardTitle>
          <span className="flex min-w-0 items-center gap-1.5 text-xs text-muted-foreground">
            <GitBranch className="size-3 shrink-0" />
            <span className="truncate" title={entry.author}>{entry.author}</span>
          </span>
        </div>
      </CardHeader>
      <CardContent className="min-w-0">
        <CardDescription
          className={cn(
            'line-clamp-2 min-h-10 [overflow-wrap:anywhere]',
            view === 'list' && 'sm:line-clamp-1 sm:min-h-5',
          )}
        >
          {localized(entry.description, i18n.language)}
        </CardDescription>
      </CardContent>
      <CardFooter className="mt-auto min-w-0 flex-wrap gap-1.5 border-0 bg-transparent pt-0">
        <Badge
          variant="secondary"
          title={entry.kinds
            .map((kind) => t(`market.kinds.${kind}`))
            .join(' · ')}
        >
          {kindLabel}
          {entry.kinds.length > 1 && (
            <span className="text-muted-foreground">
              +{entry.kinds.length - 1}
            </span>
          )}
        </Badge>
        {entry.discovery === 'github_topic' && <Badge variant="outline">{t('market.discovery.githubTopic')}</Badge>}
        {entry.tags.slice(0, 2).map((tag) => (
          <Badge variant="outline" key={tag} className="max-w-full" title={t(`market.tag.${tag}`, { defaultValue: tag })}>
            <span className="truncate">{t(`market.tag.${tag}`, { defaultValue: tag })}</span>
          </Badge>
        ))}
        <time className="ml-auto shrink-0 text-xs tabular-nums text-muted-foreground" dateTime={entry.updated_at}>
          {new Date(`${entry.updated_at}T00:00:00`).toLocaleDateString(
            i18n.language,
          )}
        </time>
      </CardFooter>
    </Card>
  )
}
