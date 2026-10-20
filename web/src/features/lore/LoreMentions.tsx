import { useEffect, useId, useMemo, useRef, useState, type ReactNode } from 'react'
import { useInfiniteQuery, useQuery } from '@tanstack/react-query'
import { ArrowLeft, ArrowUpRight, ChevronDown, FileText, Gamepad2, Info, Search } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useToolNavigation } from '@/components/Chat/tool-navigation'
import { Button } from '@/components/ui/button'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import { Field, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { EmptyState } from '@/components/common/EmptyState'
import { HighlightedText } from '@/components/common/HighlightedText'
import { InlineErrorNotice } from '@/components/common/inline-error-notice'
import { LoadingState } from '@/components/common/LoadingState'
import { searchWorkspace, type LoreItem, type WorkspaceSearchResult } from '@/lib/api'
import { withErrorLogID } from '@/lib/api-client/client'
import { workspaceFileName } from '@/lib/workspace-path'
import { findNarrativeMentions, getMentionBranches, getMentionStories, readNarrativeSource, type NarrativeMention } from './mentions-api'

const FILE_MATCH_LIMIT = 100

/** Object-scoped text evidence. Queries and previews never mutate Lore or Game selection. */
export function LoreMentions({ projectId, item }: { projectId: string; item: LoreItem }) {
  const { t, i18n } = useTranslation()
  const navigation = useToolNavigation()
  const inputId = useId()
  const [scope, setScope] = useState<'files' | 'game'>('files')
  const [query, setQuery] = useState(item.name)
  const [submitted, setSubmitted] = useState('')
  const [storySelection, setStorySelection] = useState('')
  const [branchSelection, setBranchSelection] = useState<{ story: string; branch: string } | null>(null)
  const [source, setSource] = useState<NarrativeMention | null>(null)
  const pane = useRef<HTMLElement>(null)
  const backButton = useRef<HTMLButtonElement>(null)
  const returnTarget = useRef<HTMLButtonElement | null>(null)
  const returnScroll = useRef(0)

  const storiesQuery = useQuery({
    queryKey: ['lore-mention-stories', projectId],
    queryFn: ({ signal }) => getMentionStories(projectId, signal),
    enabled: scope === 'game', retry: false,
  })
  const stories = storiesQuery.data?.stories ?? []
  const storyId = stories.find(story => story.id === storySelection)?.id
    || stories.find(story => story.id === storiesQuery.data?.current_story_id)?.id || stories[0]?.id || ''
  const branchesQuery = useQuery({
    queryKey: ['lore-mention-branches', projectId, storyId],
    queryFn: ({ signal }) => getMentionBranches(projectId, storyId, signal),
    enabled: scope === 'game' && Boolean(storyId), retry: false,
  })
  const branches = branchesQuery.data ?? []
  const branchId = branches.find(branch => branchSelection?.story === storyId && branch.id === branchSelection.branch)?.id
    || branches.find(branch => branch.current)?.id || branches[0]?.id || ''

  const filesQuery = useQuery({
    queryKey: ['lore-mention-files', projectId, submitted],
    queryFn: ({ signal }) => searchWorkspace(projectId, submitted, FILE_MATCH_LIMIT, { textMentions: true, signal }),
    enabled: scope === 'files' && Boolean(submitted), retry: false,
  })
  const narrativeQuery = useInfiniteQuery({
    queryKey: ['lore-mention-narrative', projectId, storyId, branchId, submitted],
    initialPageParam: '',
    queryFn: ({ signal, pageParam }) => findNarrativeMentions(projectId, storyId, branchId, submitted, pageParam, signal),
    getNextPageParam: page => page.has_more ? page.before_cursor : undefined,
    enabled: scope === 'game' && Boolean(storyId && branchId && submitted), retry: false,
  })
  const fileGroups = useMemo(() => {
    const grouped = new Map<string, WorkspaceSearchResult[]>()
    for (const hit of filesQuery.data ?? []) grouped.set(hit.path, [...(grouped.get(hit.path) ?? []), hit])
    return [...grouped]
  }, [filesQuery.data])
  const narrativeHits = narrativeQuery.data?.pages.flatMap(page => page.hits) ?? []
  const scannedTurns = narrativeQuery.data?.pages.reduce((count, page) => count + page.scanned_turns, 0) ?? 0
  const metadataLoading = scope === 'game' && (storiesQuery.isPending || Boolean(storyId && branchesQuery.isPending))
  const queryState = scope === 'files' ? filesQuery : narrativeQuery
  const error = scope === 'game' ? storiesQuery.error || branchesQuery.error || narrativeQuery.error : filesQuery.error
  const loading = Boolean(submitted) && (metadataLoading || queryState.isFetching)
  const hasResults = scope === 'files' ? fileGroups.length > 0 : narrativeHits.length > 0

  useEffect(() => {
    setSource(null)
    returnScroll.current = 0
  }, [projectId, storyId, branchId])

  useEffect(() => {
    if (source) {
      pane.current?.scrollTo({ top: 0 })
      backButton.current?.focus({ preventScroll: true })
    } else {
      pane.current?.scrollTo({ top: returnScroll.current })
      returnTarget.current?.focus({ preventScroll: true })
    }
  }, [source])

  const find = () => {
    setSource(null)
    const next = query.trim()
    setSubmitted(next)
    if (next && next === submitted) {
      if (scope === 'game' && (storiesQuery.isError || !storyId)) void storiesQuery.refetch()
      else if (scope === 'game' && (branchesQuery.isError || !branchId)) void branchesQuery.refetch()
      else void queryState.refetch()
    }
    console.info('[lore-mentions] finding text mentions', { projectId, itemId: item.id, scope, storyId, branchId })
  }
  const openSource = (hit: NarrativeMention, target: HTMLButtonElement) => {
    returnTarget.current = target
    returnScroll.current = pane.current?.scrollTop ?? 0
    setSource(hit)
  }
  const formatTime = (timestamp: string) => new Date(timestamp).toLocaleString(i18n.language)

  return <section ref={pane} className="min-h-0 min-w-0 flex-1 overflow-y-auto p-4" aria-label={t('lore.mentions.tab')} data-testid="lore-mentions" onKeyDown={event => {
    if (event.key === 'Escape' && source) { event.preventDefault(); setSource(null) }
  }}>
    <div hidden={Boolean(source)}>
      <form onSubmit={event => { event.preventDefault(); find() }} className="flex items-end gap-2">
        <Field className="min-w-0 flex-1">
          <FieldLabel htmlFor={inputId}>{t('lore.mentions.query')}</FieldLabel>
          <Input id={inputId} value={query} maxLength={1024} onChange={event => { setQuery(event.target.value); setSubmitted(''); setSource(null) }} />
        </Field>
        <Button type="submit" disabled={!query.trim()}><Search data-icon="inline-start" />{t('lore.mentions.find')}</Button>
      </form>
      <div className="my-4 flex min-w-0 flex-wrap items-center gap-3">
        <ToggleGroup type="single" value={scope} variant="outline" spacing={0} aria-label={t('lore.mentions.scope')} onValueChange={value => {
          if (value === 'files' || value === 'game') { setScope(value); setSource(null); returnScroll.current = 0 }
        }}>
          <ToggleGroupItem value="files"><FileText data-icon="inline-start" />{t('lore.mentions.files')}</ToggleGroupItem>
          <ToggleGroupItem value="game"><Gamepad2 data-icon="inline-start" />{t('lore.mentions.game')}</ToggleGroupItem>
        </ToggleGroup>
        {scope === 'files' ? <span className="text-xs text-muted-foreground">{t('lore.mentions.projectScope')}</span> : <>
          {stories.length > 1 ? <Select value={storyId} onValueChange={value => { setStorySelection(value); setBranchSelection(null); setSource(null) }}>
            <SelectTrigger className="min-w-0 max-w-full" aria-label={t('lore.mentions.story')}><SelectValue /></SelectTrigger>
            <SelectContent><SelectGroup>{stories.map(story => <SelectItem key={story.id} value={story.id}>{story.title}</SelectItem>)}</SelectGroup></SelectContent>
          </Select> : <span className="min-w-0 break-words text-xs text-muted-foreground">{stories[0]?.title}</span>}
          {branches.length > 0 && <div className="flex min-w-0 max-w-full items-center gap-2">
            <span className="shrink-0 text-xs text-muted-foreground">{t('lore.mentions.branch')}</span>
            <Select value={branchId} onValueChange={value => { setBranchSelection({ story: storyId, branch: value }); setSource(null) }}>
              <SelectTrigger className="min-w-0 max-w-full" aria-label={t('lore.mentions.branch')}><SelectValue /></SelectTrigger>
              <SelectContent><SelectGroup>{branches.map(branch => <SelectItem key={branch.id} value={branch.id}>{branch.title || branch.id}</SelectItem>)}</SelectGroup></SelectContent>
            </Select>
          </div>}
        </>}
      </div>
      <p className="mb-4 flex items-start gap-2 text-xs leading-relaxed text-muted-foreground"><Info className="mt-0.5 size-3.5 shrink-0" />{t(scope === 'files' ? 'lore.mentions.fileHint' : 'lore.mentions.gameHint')}</p>
      {loading && <LoadingState variant="inline" label={t('lore.mentions.loading')} />}
      {error && <div className="my-3 flex flex-col items-start gap-2"><InlineErrorNotice message={withErrorLogID(t('lore.mentions.failed'), error)} /><Button variant="outline" size="sm" onClick={find}>{t('common.retry')}</Button></div>}
      <div aria-live="polite" className="my-3 text-xs text-muted-foreground">
        {submitted && !metadataLoading && queryState.data && t(scope === 'files' ? 'lore.mentions.fileCount' : 'lore.mentions.turnCount', {
          count: scope === 'files' ? filesQuery.data?.length ?? 0 : scannedTurns,
          sources: scope === 'files' ? fileGroups.length : narrativeHits.length,
        })}
      </div>
      {!loading && !error && !hasResults && <EmptyState icon={Search}
        title={t(!submitted ? 'lore.mentions.idle' : scope === 'game' && !storyId ? 'lore.mentions.noStories' : 'lore.mentions.empty')}
        description={t(!submitted ? 'lore.mentions.idleHint' : scope === 'game' && narrativeQuery.hasNextPage ? 'lore.mentions.moreHint' : 'lore.mentions.emptyHint')} />}
      {submitted && scope === 'files' && fileGroups.map(([path, hits]) => <MentionGroup key={`${submitted}:${path}`} title={workspaceFileName(path)} meta={path} preview={hits[0].preview} query={submitted} count={hits.length}>
        {hits.map((hit, index) => <div key={`${hit.line}:${hit.column}:${index}`} className="flex flex-col gap-2 border-l-2 border-border py-2 pl-3">
          <div className="text-xs text-muted-foreground">{t('lore.mentions.line', { line: hit.line })}</div>
          <p className="whitespace-pre-wrap break-words text-sm leading-relaxed"><HighlightedText text={hit.preview} query={submitted} /></p>
          <Button variant="outline" size="sm" className="self-start" disabled={!navigation} onClick={() => navigation?.open({ kind: 'workspace_file', path, search: { query: submitted, line: hit.line } })}>
            {t('lore.mentions.openFile')}<ArrowUpRight data-icon="inline-end" />
          </Button>
        </div>)}
      </MentionGroup>)}
      {submitted && scope === 'game' && narrativeHits.map(hit => <MentionGroup key={hit.turn_id} title={t('lore.mentions.turnAt', { time: formatTime(hit.timestamp) })} meta={branches.find(branch => branch.id === branchId)?.title || branchId} preview={hit.matches[0].preview} query={submitted} count={hit.matches.length}>
        {hit.matches.map((match, index) => <div key={index} className="flex flex-col gap-2 border-l-2 border-border py-2 pl-3">
          <div className="text-xs text-muted-foreground">{t('lore.mentions.line', { line: match.line })}</div>
          <p className="whitespace-pre-wrap break-words text-sm leading-relaxed"><HighlightedText text={match.preview} query={submitted} /></p>
        </div>)}
        {hit.truncated && <p className="text-xs text-muted-foreground">{t('lore.mentions.turnLimit')}</p>}
        <Button variant="outline" size="sm" className="self-start" onClick={event => openSource(hit, event.currentTarget)}>{t('lore.mentions.openTurn')}<ArrowUpRight data-icon="inline-end" /></Button>
      </MentionGroup>)}
      {scope === 'files' && filesQuery.data?.length === FILE_MATCH_LIMIT && <p role="status" className="mt-4 text-xs text-muted-foreground">{t('lore.mentions.fileLimit', { count: FILE_MATCH_LIMIT })}</p>}
      {scope === 'game' && submitted && narrativeQuery.hasNextPage && <Button className="mt-4" variant="outline" disabled={narrativeQuery.isFetching} onClick={() => void narrativeQuery.fetchNextPage()}>{t('lore.mentions.more')}</Button>}
    </div>
    {source && <div className="flex min-w-0 flex-col gap-4">
      <div className="sticky -top-4 -mx-4 z-10 flex flex-wrap items-center justify-between gap-3 bg-background px-4 py-2"><Button ref={backButton} variant="ghost" size="sm" onClick={() => setSource(null)}><ArrowLeft data-icon="inline-start" />{t('lore.mentions.back')}</Button><span className="text-xs text-muted-foreground">{t('lore.mentions.readOnly')}</span></div>
      <h3 className="text-sm font-medium">{t('lore.mentions.turnAt', { time: formatTime(source.timestamp) })}</h3>
      <LoreNarrativeSource projectId={projectId} storyId={storyId} branchId={branchId} hit={source} query={submitted} />
    </div>}
  </section>
}

function MentionGroup({ title, meta, preview, query, count, children }: { title: string; meta: string; preview: string; query: string; count: number; children: ReactNode }) {
  const { t } = useTranslation()
  return <Collapsible className="border-b border-border" data-testid="lore-mention-group">
    <CollapsibleTrigger asChild><Button variant="ghost" className="group h-auto w-full flex-col items-stretch gap-1 rounded-none px-0 py-4 text-left whitespace-normal">
      <span className="flex items-start gap-2"><span className="line-clamp-2 min-w-0 flex-1 break-words font-medium" title={title}>{title}</span><span className="shrink-0 text-xs text-muted-foreground">{t('lore.mentions.matches', { count })}</span><ChevronDown data-icon="inline-end" className="transition-transform group-data-[state=closed]:-rotate-90" /></span>
      <span className="break-all text-xs font-normal text-muted-foreground">{meta}</span>
      <span className="mt-1 whitespace-pre-wrap break-words font-normal leading-relaxed text-muted-foreground"><HighlightedText text={preview} query={query} /></span>
    </Button></CollapsibleTrigger>
    <CollapsibleContent className="flex flex-col gap-3 pb-4 pl-2">{children}</CollapsibleContent>
  </Collapsible>
}

function LoreNarrativeSource({ projectId, storyId, branchId, hit, query }: { projectId: string; storyId: string; branchId: string; hit: NarrativeMention; query: string }) {
  const { t } = useTranslation()
  const article = useRef<HTMLElement>(null)
  const source = useQuery({
    queryKey: ['lore-narrative-source', projectId, storyId, branchId, hit.turn_id, hit.source_revision],
    queryFn: ({ signal }) => readNarrativeSource(projectId, storyId, branchId, hit.turn_id, hit.source_revision, signal),
    retry: false, gcTime: 0,
  })
  useEffect(() => {
    article.current?.querySelector('mark')?.scrollIntoView({ block: 'center' })
  }, [source.data])
  return <>
    <p className="text-xs text-muted-foreground">{t('lore.mentions.readOnlyHint')}</p>
    {source.isPending && <LoadingState variant="panel" label={t('lore.mentions.loading')} />}
    {source.error && <div className="flex flex-col items-start gap-2"><InlineErrorNotice message={withErrorLogID(t('lore.mentions.sourceFailed'), source.error)} /><Button variant="outline" size="sm" onClick={() => void source.refetch()}>{t('common.retry')}</Button></div>}
    {source.data && !source.error && <article ref={article} data-testid="lore-mention-source" className="whitespace-pre-wrap break-words text-sm leading-8"><HighlightedText text={source.data.narrative} query={query} /></article>}
  </>
}
