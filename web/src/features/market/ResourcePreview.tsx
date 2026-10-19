import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { ThemedMarkdownRenderer } from '@/components/common/MarkdownRenderer'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { exchange } from './api'

type ResourceFiles = {
  items?: { id: string; name: string; brief_description?: string }[]
  files: { path: string; bytes: number }[]
  content?: string
  path?: string
  truncated: boolean
  binary: boolean
}

// Read content fields from validated payloads; other files remain available as source.
function readableContent(result: ResourceFiles): string | undefined {
  if (!result.path || result.binary || result.truncated) return
  if (/\.(md|txt)$/i.test(result.path)) return result.content?.replace(/^---\r?\n[\s\S]*?\r?\n---\r?\n/, '')
  if (!result.path.endsWith('.json')) return
  try {
    const value = JSON.parse(result.content || '')
    if (typeof value?.content === 'string') return value.content
    if (typeof value?.prompt === 'string') return value.prompt
    if (Array.isArray(value?.slots)) return value.slots.map((slot: { name?: string; content?: string }) =>
      typeof slot.content === 'string' ? `${slot.name ? `### ${slot.name}\n\n` : ''}${slot.content}` : '',
    ).filter(Boolean).join('\n\n') || undefined
  } catch { /* A truncated or non-JSON file is still readable as source. */ }
}

export function ResourcePreview({ previewID, candidateID, resourceID }: {
  previewID: string
  candidateID: string
  resourceID: string
}) {
  const { t } = useTranslation()
  const [selected, setSelected] = useState('')
  const [itemID, setItemID] = useState('')
  const [query, setQuery] = useState('')
  const [result, setResult] = useState<ResourceFiles>()
  const [error, setError] = useState('')
  useEffect(() => {
    let alive = true
    setResult(undefined)
    setError('')
    const query = new URLSearchParams({ candidate_id: candidateID, resource_id: resourceID, path: selected, item_id: itemID })
    void exchange<ResourceFiles>(`/previews/${previewID}/files?${query}`).then((value) => {
      if (!alive) return
      if (!selected && value.files.length) {
        if (value.items?.length) setItemID(value.items[0].id)
        setSelected(value.files.find((file) => file.path === 'SKILL.md')?.path || value.files[0].path)
      } else setResult(value)
    }).catch((cause) => {
      console.error('[market] failed to read resource preview', cause)
      if (alive) setError(t('market.errors.operationFailed'))
    })
    return () => { alive = false }
  }, [previewID, candidateID, resourceID, selected, itemID, t])
  if (error) return <p role="alert" className="text-sm text-destructive">{error}</p>
  if (!result) return <p role="status" className="text-sm text-muted-foreground">{t('market.contents.loading')}</p>
  const content = readableContent(result)
  const visibleItems = result.items?.filter(item => `${item.name} ${item.brief_description || ''}`.toLocaleLowerCase().includes(query.trim().toLocaleLowerCase()))
  const source = <pre className="max-h-96 overflow-auto whitespace-pre-wrap rounded-md bg-muted p-3 text-xs [overflow-wrap:anywhere]">{result.content}</pre>
  return <div className="min-w-0 space-y-3">
    {result.items && <div className="space-y-2">
      <p className="text-sm text-muted-foreground">{t('market.contents.collectionCount', { count: result.items.length })}</p>
      <Input aria-label={t('market.contents.searchItems')} placeholder={t('market.contents.searchItems')} value={query} onChange={event => setQuery(event.target.value)} />
      {visibleItems?.length === 0 && <p className="text-sm text-muted-foreground">{t('market.contents.noMatches')}</p>}
      <div className="max-h-48 overflow-y-auto rounded-md border" role="list" aria-label={t('market.contents.collectionItems')}>
        {visibleItems?.map(item => <Button key={item.id} variant={itemID === item.id ? 'secondary' : 'ghost'} aria-pressed={itemID === item.id} className="h-auto w-full justify-start whitespace-normal p-3 text-left [overflow-wrap:anywhere]" onClick={() => setItemID(item.id)}>{item.name}</Button>)}
      </div>
    </div>}
    {result.files.length > 1 && <div className="flex max-h-40 flex-col overflow-auto rounded-md border">
      {result.files.map((file) => <Button key={file.path} variant={selected === file.path ? 'secondary' : 'ghost'} className="h-auto justify-start whitespace-normal p-2 text-left text-xs [overflow-wrap:anywhere]" onClick={() => setSelected(file.path)}>{file.path}</Button>)}
    </div>}
    {result.binary ? <p className="text-sm text-muted-foreground">{t('market.import.binaryFile')}</p> : content ? <>
      <ThemedMarkdownRenderer content={content} className="text-sm leading-7 [overflow-wrap:anywhere]" components={{ img: ({ alt }) => <span>{alt}</span> }} />
      <details className="text-xs text-muted-foreground"><summary className="cursor-pointer py-2">{t('market.contents.source')}</summary>{source}</details>
    </> : source}
    {result.truncated && <p className="text-xs text-muted-foreground">{t('market.import.truncated')}</p>}
  </div>
}
