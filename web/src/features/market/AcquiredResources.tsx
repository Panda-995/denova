import { useQuery } from '@tanstack/react-query'
import { Download, Package, RefreshCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from '@/components/ui/card'
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import { getBooks } from '@/lib/api'
import type { Installation } from './api'

/** Summary cards only; installation management belongs to the selected detail view. */
export function AcquiredResources({ installations, onOpen, onDiscover }: {
  installations: Installation[]
  onOpen: (installationID: string, trigger: HTMLButtonElement) => void
  onDiscover: () => void
}) {
  const { t, i18n } = useTranslation()
  const books = useQuery({ queryKey: ['resources', 'books'], queryFn: getBooks, enabled: installations.some(item => !!item.project_id) })

  if (!installations.length) return <Empty>
    <EmptyHeader>
      <EmptyMedia variant="icon"><Download /></EmptyMedia>
      <EmptyTitle>{t('market.acquiredEmpty')}</EmptyTitle>
      <EmptyDescription>{t('market.acquiredHelp')}</EmptyDescription>
    </EmptyHeader>
    <Button onClick={onDiscover}>{t('market.discover')}</Button>
  </Empty>

  return <div className="grid min-w-0 auto-rows-fr grid-cols-[repeat(auto-fill,minmax(min(100%,18rem),1fr))] gap-3">
    {installations.map(item => {
      const translation = item.package.translations?.[i18n.language]
      const name = translation?.name || item.package.name
      const description = translation?.description || item.package.description
      const kinds = [...new Set(item.bindings.map(binding => binding.local.kind))]
      const targetName = item.project_id
        ? books.data?.find(book => book.project_id === item.project_id)?.name || item.project_id
        : t('market.import.global')
      const updateAvailable = item.tracking === 'tracked' && item.remote_state === 'update_available'
      const state = item.tracking === 'detached' ? 'detached'
        : item.remote_state === 'check_failed' || item.remote_state === 'identity_changed' ? item.remote_state
          : item.local_state || 'unchanged'

      return <Card key={item.installation_id} size="sm" data-testid="market-installation-card"
        className="relative min-w-0 transition-shadow hover:ring-foreground/25 focus-within:ring-2 focus-within:ring-ring">
        <CardHeader className="min-w-0 gap-2">
          <CardTitle className="flex min-w-0 items-start gap-2">
            <Package className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
            <button title={name} className="min-w-0 flex-1 text-left after:absolute after:inset-0 focus:outline-none"
              onClick={event => onOpen(item.installation_id, event.currentTarget)}>
              <span className="line-clamp-2 min-h-10 [overflow-wrap:anywhere]">{name}</span>
            </button>
          </CardTitle>
          <CardDescription className="line-clamp-2 h-10 [overflow-wrap:anywhere]">{description || kinds.map(kind => t(`market.kinds.${kind}`)).join(' · ')}</CardDescription>
        </CardHeader>
        <CardContent className="flex min-w-0 flex-col items-start gap-2">
          <Badge variant={updateAvailable ? 'default' : 'secondary'} className="max-w-full">
            {updateAvailable && <RefreshCw data-icon="inline-start" />}
            <span className="truncate">{t(updateAvailable ? 'market.updateAvailable' : `market.states.${state}`)}</span>
          </Badge>
          <div className="flex h-5 min-w-0 max-w-full items-center gap-1.5">
            {kinds.length > 0 && <Badge variant="outline" className="min-w-0"><span className="truncate">{t(`market.kinds.${kinds[0]}`)}</span></Badge>}
            {kinds.length > 1 && <Badge variant="outline" className="shrink-0">+{kinds.length - 1}</Badge>}
          </div>
        </CardContent>
        <CardFooter className="mt-auto min-w-0 gap-2 px-3 py-2 text-xs text-muted-foreground">
          <span className="min-w-0 flex-1 truncate" title={targetName}>{targetName}</span>
          <span className="shrink-0">{t('market.acquired.resourceCount', { count: item.bindings.length })}</span>
          {item.package.version && <span className="max-w-24 truncate" title={item.package.version}>v{item.package.version}</span>}
        </CardFooter>
      </Card>
    })}
  </div>
}
