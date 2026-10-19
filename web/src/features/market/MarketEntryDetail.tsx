import type { ReactNode } from 'react'
import { ArrowLeft, Download, ExternalLink, Package } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { localized, type MarketEntry, type Source } from './api'
import type { ResourceInput } from './ImportSourceFields'

export function MarketEntryDetail({ detail, acquired, onBack, onImport, onManage, updates }: {
  updates?: ReactNode
  detail: MarketEntry
  acquired: number
  onBack: () => void
  onImport: (source: Source, initialInput?: ResourceInput) => void
  onManage: () => void
}) {
  const { t, i18n } = useTranslation()
  return <div className="h-full min-h-0 overflow-auto @container" data-testid="market-entry-detail">
    <article className="mx-auto max-w-6xl space-y-6 p-4 @xl:p-6 @4xl:p-8">
      <Button variant="ghost" className="-ml-3" onClick={onBack}><ArrowLeft data-icon="inline-start" />{t('market.back')}</Button>
      <header className="space-y-4">
        <div className="flex items-start gap-3">
          <div className="flex size-11 shrink-0 items-center justify-center rounded-lg border bg-muted/30"><Package className="size-5 text-muted-foreground" /></div>
          <div className="min-w-0 space-y-2">
            <h1 className="text-2xl font-semibold tracking-tight [overflow-wrap:anywhere]">{localized(detail.name, i18n.language)}</h1>
            <p className="text-sm text-muted-foreground [overflow-wrap:anywhere]">{detail.author} · {new Date(`${detail.updated_at}T00:00:00`).toLocaleDateString(i18n.language)}</p>
            <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-xs leading-relaxed text-muted-foreground [overflow-wrap:anywhere]">
              <a className="inline-flex min-w-0 items-center gap-1 underline-offset-4 hover:underline" href={detail.source.url} target="_blank" rel="noreferrer"><span className="min-w-0 [overflow-wrap:anywhere]">{detail.source.url?.replace(/^https:\/\//, '')}</span><ExternalLink className="size-3 shrink-0" /></a>
              {detail.source.ref && <span>· {detail.source.ref}</span>}
              {detail.source.path && <span className="min-w-0 font-mono">· {detail.source.path}</span>}
            </div>
          </div>
        </div>
        <p className="max-w-3xl whitespace-pre-wrap leading-relaxed [overflow-wrap:anywhere]">{localized(detail.description, i18n.language)}</p>
        <div className="flex flex-wrap gap-2">
          {detail.discovery === 'github_topic' && <Badge variant="outline">{t('market.discovery.githubTopic')}</Badge>}
          {detail.tags.map((tag) => <Badge key={tag} variant="outline">{t(`market.tag.${tag}`, { defaultValue: tag })}</Badge>)}
        </div>
        {detail.discovery === 'github_topic' && <p className="text-sm text-muted-foreground">{t('market.discovery.githubTopicHelp')}</p>}
      </header>
      <div className="grid min-w-0 items-start gap-6 border-t pt-6 @4xl:grid-cols-[minmax(0,1fr)_17rem] @4xl:gap-8">
        <aside className="space-y-3 @4xl:space-y-4 @4xl:rounded-xl @4xl:border @4xl:bg-muted/20 @4xl:p-4 @4xl:sticky @4xl:top-6 @4xl:col-start-2 @4xl:row-start-1">
          <div className="hidden space-y-2 @4xl:block">
            <h2 className="text-sm font-semibold">{t('market.contents.get')}</h2>
            <p className="text-xs leading-relaxed text-muted-foreground">{t('market.contents.selectHelp')}</p>
          </div>
          {updates}
          <Button variant={acquired > 0 ? 'outline' : 'default'} className="h-auto min-h-9 w-full whitespace-normal" onClick={() => onImport(detail.source)}>
            <Download data-icon="inline-start" />{t(acquired > 0 ? 'market.update.importElsewhere' : 'market.contents.get')}
          </Button>
          <Button variant="outline" className="h-auto min-h-9 w-full whitespace-normal" onClick={() => onImport(detail.source, { kind: 'file' })}>{t('market.import.fromFile')}</Button>
          {acquired > 0 && <Button variant="outline" className="w-full" onClick={onManage}>{t('market.contents.acquired', { count: acquired })}</Button>}
          {detail.compatibility && <div className="space-y-2 @4xl:border-t @4xl:pt-4"><h3 className="hidden text-xs font-medium @4xl:block">{t('market.contents.compatibility')}</h3><p className="whitespace-pre-wrap text-xs leading-relaxed text-muted-foreground">{localized(detail.compatibility, i18n.language)}</p></div>}
        </aside>
        <div className="min-w-0 space-y-7 @4xl:col-start-1 @4xl:row-start-1">
          <section className="min-w-0 space-y-4" aria-label={t('market.contents.title')}>
            <div className="space-y-1"><h2 className="font-semibold">{t('market.contents.title')}</h2><p className="text-sm text-muted-foreground">{t('market.contents.help')}</p></div>
            <ul className="flex flex-wrap gap-2">
              {detail.kinds.map((kind) => <li key={kind}><Badge variant="secondary">{t(`market.kinds.${kind}`)}</Badge></li>)}
            </ul>
          </section>
          {detail.usage && <section className="space-y-3 border-t pt-6"><h2 className="font-semibold">{t('market.contents.usage')}</h2><p className="whitespace-pre-wrap text-sm leading-7 text-muted-foreground [overflow-wrap:anywhere]">{localized(detail.usage, i18n.language)}</p></section>}
        </div>
      </div>
    </article>
  </div>
}
