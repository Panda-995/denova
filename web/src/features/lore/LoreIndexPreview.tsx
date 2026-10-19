import { useEffect, useRef, useState } from 'react'
import { Eye, FileCode2, RefreshCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { Skeleton } from '@/components/ui/skeleton'
import { ThemedMarkdownRenderer } from '@/components/common/MarkdownRenderer'
import { InlineErrorNotice } from '@/components/common/inline-error-notice'
import { ScrollToTopButton } from '@/components/common/ScrollToTopButton'
import { ContextCopyButton } from '@/components/Chat/ContextCopyButton'
import { previewLoreIndex } from '@/lib/api-client/lore'
import type { LoreIndexGuide, LoreIndexPreview as Preview, LoreItem } from '@/lib/api-client/types'

/** Keeps the last result during a debounce; cancelled requests cannot overwrite
 * newer edits. Revision tracks item-only writes even when query structural
 * sharing preserves the guide object. The renderer is shared with injection. */
export function useLoreIndexPreview(projectId: string, guide: LoreIndexGuide | undefined, items: LoreItem[], revision: string | undefined) {
  const [preview, setPreview] = useState<Preview | null>(null)
  const [pending, setPending] = useState(true)
  const [error, setError] = useState(false)
  useEffect(() => {
    if (!guide) return
    let cancelled = false
    setPending(true)
    const timer = setTimeout(() => {
      void previewLoreIndex(projectId, guide).then(result => {
        if (cancelled) return
        setPreview(result)
        setError(false)
      }).catch(cause => {
        if (cancelled) return
        console.error('[lore-index] live preview failed', { projectId, cause })
        setError(true)
      }).finally(() => { if (!cancelled) setPending(false) })
    }, 200)
    return () => { cancelled = true; clearTimeout(timer) }
  }, [projectId, guide, items, revision])
  return { preview, pending, error }
}

export function LoreIndexPreview({ preview, pending, error }: ReturnType<typeof useLoreIndexPreview>) {
  const { t, i18n } = useTranslation()
  const scrollRef = useRef<HTMLDivElement>(null)
  const [format, setFormat] = useState('read')
  const markdown = preview?.markdown ?? ''
  const failed = error || preview?.over_budget
  const tokenEstimate = preview ? t('lore.index.estimatedTokens', { value: new Intl.NumberFormat(i18n.language).format(preview.token_estimate) }) : ''
  return <aside className="@container/lore-preview flex h-full min-h-0 min-w-0 flex-col bg-muted/20" aria-label={t('lore.index.preview')} data-testid="lore-index-live-preview" aria-busy={pending}>
    <header className="flex min-w-0 shrink-0 items-center gap-2 border-b px-3 py-2">
      <h2 className="hidden shrink-0 text-sm font-semibold @xl/lore-preview:block" title={t('lore.index.previewHint')}>{t('lore.index.preview')}</h2>
      <ToggleGroup className="shrink-0" size="sm" spacing={1} type="single" value={format} onValueChange={value => { if (value) setFormat(value) }} aria-label={t('lore.index.previewFormat')}>
        <ToggleGroupItem value="read" aria-label={t('lore.index.rendered')} title={t('lore.index.rendered')}><Eye /><span className="hidden @md/lore-preview:inline">{t('lore.index.rendered')}</span></ToggleGroupItem>
        <ToggleGroupItem value="source" aria-label={t('lore.index.source')} title={t('lore.index.source')}><FileCode2 /><span className="hidden @md/lore-preview:inline">{t('lore.index.source')}</span></ToggleGroupItem>
      </ToggleGroup>
      <div className="ml-auto flex min-w-0 flex-1 items-center justify-end gap-1">
        <span className="shrink-0 text-muted-foreground" title={t(pending ? 'lore.index.updating' : 'lore.index.live')}><RefreshCw className="size-3.5 data-[pending=true]:animate-spin" data-pending={pending} aria-label={t(pending ? 'lore.index.updating' : 'lore.index.live')} /></span>
        {preview && !failed && <span className="min-w-0 truncate text-xs tabular-nums text-muted-foreground" title={tokenEstimate} data-testid="lore-index-token-estimate">{tokenEstimate}</span>}
        <ContextCopyButton content={failed ? '' : markdown} label={t('lore.index.copy')} copiedLabel={t('lore.index.copied')} failedLabel={t('lore.index.copyFailed')} />
      </div>
    </header>
    <div className="relative min-h-0 flex-1">
    <div ref={scrollRef} className="h-full overflow-y-auto p-5" data-testid="lore-index-preview">
      {failed ? <InlineErrorNotice message={t(preview?.over_budget ? 'lore.index.overBudget' : 'lore.index.previewFailed')} /> : !preview ? <div className="flex flex-col gap-4"><Skeleton className="h-6 w-1/2" /><Skeleton className="h-24 w-full" /></div> : !markdown ? <p className="text-sm text-muted-foreground">{t('lore.index.previewEmpty')}</p> : format === 'source' ? <pre className="whitespace-pre-wrap break-words font-mono text-xs leading-relaxed">{markdown}</pre> : <ThemedMarkdownRenderer content={markdown} className="break-words text-sm [&_h1]:text-xl! [&_h2]:text-base! [&_h3]:text-sm! [&_p]:text-sm! [&_li]:text-sm! [&_blockquote]:text-muted-foreground!" />}
    </div>
    <ScrollToTopButton scrollRef={scrollRef} />
    </div>
  </aside>
}
