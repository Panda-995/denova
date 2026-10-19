import { useCallback, useEffect, useRef, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { getLoreIndex, updateLoreIndex } from '@/lib/api-client/lore'
import type { LoreIndexGuide, LoreIndexSnapshot } from '@/lib/api-client/types'
import { useResourceAutosave } from '@/hooks/use-resource-autosave'
import { isRevisionConflict } from '@/lib/revision-conflict'
import { rebaseJSONWithRecovery } from '@/lib/autosave/rebase-with-recovery'
import { rebaseJSONValue } from '@/lib/three-way-rebase'
import { loreIndexKey, useLoreIndex } from './use-lore-index'
import { notifyLoreUpdated } from './events'

interface IndexDraft { id: string; updated_at: string; guide: LoreIndexGuide }
const toDraft = (snapshot: LoreIndexSnapshot): IndexDraft => ({ id: 'index', updated_at: snapshot.revision, guide: snapshot.guide })

/** The document uses the existing save lane and conflict archive. File revision
 * conflicts caused by item edits merge without replacing those item records. */
export function useLoreIndexDocument(projectId: string) {
  const { t } = useTranslation()
  const client = useQueryClient()
  const query = useLoreIndex(projectId)
  const [draft, setDraft] = useState<IndexDraft | null>(null)
  const [baseline, setBaseline] = useState<IndexDraft | null>(null)
  const current = useRef(draft)
  current.current = draft
  const base = useRef(baseline)
  base.current = baseline
  const groups = draft?.guide.groups ?? []
  const valid = groups.every(group => group.name.trim()) && new Set(groups.map(g => g.name.trim().toLowerCase())).size === groups.length
  const reportError = useCallback((error: unknown) => {
    console.error('[lore-index] save failed', { projectId, error })
    toast.error(error instanceof Error ? error.message : t('editor.saveFailed'))
  }, [projectId, t])
  const autosave = useResourceAutosave<IndexDraft, { guide: LoreIndexGuide }, IndexDraft>({
    draft, active: Boolean(draft), valid, scopeKey: projectId,
    makePayload: value => ({ guide: value.guide }),
    signature: value => JSON.stringify(value.guide),
    save: async (_id, payload, revision) => toDraft(await updateLoreIndex(projectId, payload.guide, revision ?? '')),
    baselineFromSaved: saved => saved,
    resolveConflict: async ({ error, baseline: previous, draft: submitted }) => {
      if (!isRevisionConflict(error)) return null
      const latest = toDraft(await getLoreIndex(projectId))
      const guide = await rebaseJSONWithRecovery({
        resource: 'lore_index', scope: projectId, id: 'index',
        baseline: { revision: previous?.updated_at ?? submitted.updated_at, value: previous?.guide ?? submitted.guide },
        local: { revision: submitted.updated_at, value: submitted.guide },
        external: { revision: latest.updated_at, value: latest.guide },
      })
      return { payload: { guide }, baseRevision: latest.updated_at }
    },
    onSaved: (saved, _mode, submitted) => {
      const next = rebaseJSONValue(submitted, current.current ?? submitted, saved)
      current.current = next
      base.current = saved
      setDraft(next)
      setBaseline(saved)
      client.setQueryData(loreIndexKey(projectId), { guide: saved.guide, revision: saved.updated_at })
      notifyLoreUpdated({ projectId, source: 'lore-index' })
    },
    onAutoSaveError: reportError,
  })
  useEffect(() => { autosave.resetBaseline(baseline) }, [autosave.resetBaseline, baseline])
  useEffect(() => {
    if (!query.data) return
    const canonical = toDraft(query.data)
    if (base.current?.updated_at === canonical.updated_at) return
    let cancelled = false
    const reconcile = async () => {
      const previous = base.current
      const local = current.current
      let guide = previous && local ? await rebaseJSONWithRecovery({
        resource: 'lore_index', scope: projectId, id: 'index',
        baseline: { revision: previous.updated_at, value: previous.guide },
        local: { revision: local.updated_at, value: local.guide },
        external: { revision: canonical.updated_at, value: canonical.guide },
      }) : canonical.guide
      if (cancelled) return
      if (local && current.current && current.current !== local) guide = rebaseJSONValue(local.guide, current.current.guide, guide)
      const next = { ...canonical, guide }
      current.current = next
      base.current = canonical
      setDraft(next)
      setBaseline(canonical)
    }
    void reconcile().catch(reportError)
    return () => { cancelled = true }
  }, [projectId, query.data, reportError])
  const flush = useCallback(async () => {
    if (!current.current) return !query.isError
    if (!valid) { toast.error(t('lore.index.invalid')); return false }
    try { await autosave.saveNow('manual'); return true } catch (error) { reportError(error); return false }
  }, [autosave.saveNow, query.isError, reportError, t, valid])
  return {
    ...query, guide: draft?.guide, valid, status: autosave.status, saveError: autosave.error, flush,
    setGuide: (guide: LoreIndexGuide) => setDraft(previous => previous ? { ...previous, guide } : previous),
  }
}
