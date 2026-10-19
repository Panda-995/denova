import { useEffect, useMemo, useRef } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { workspaceChangePaths, type WorkspaceChangeEvent } from '@/features/changes/types'
import { isLoreItemsPath } from '@/lib/workspace-path'
import { getLoreCategories } from '@/lib/api-client/lore'
import { DEFAULT_LORE_CATEGORIES, knowledgeSections } from './knowledge-sections'
import { LORE_UPDATED_EVENT, type LoreUpdatedDetail } from './events'

export const loreCategoriesKey = (projectId: string) => ['lore-categories', projectId] as const

/** Category definitions belong to the project and are shared by every Lore view. */
export function useLoreCategories(projectId: string, refreshSignal = 0) {
  const client = useQueryClient()
  const query = useQuery({
    queryKey: loreCategoriesKey(projectId),
    queryFn: () => getLoreCategories(projectId),
    enabled: Boolean(projectId),
    staleTime: 30_000,
  })
  useEffect(() => {
    const onUpdated = (event: Event) => {
      if ((event as CustomEvent<LoreUpdatedDetail>).detail.projectId === projectId) {
        void client.invalidateQueries({ queryKey: loreCategoriesKey(projectId) }, { cancelRefetch: false })
      }
    }
    const onWorkspaceChange = (event: Event) => {
      const detail = (event as CustomEvent<WorkspaceChangeEvent>).detail
      if (detail?.project_id === projectId && (detail.resync || workspaceChangePaths(detail).some(isLoreItemsPath))) {
        void client.invalidateQueries({ queryKey: loreCategoriesKey(projectId) }, { cancelRefetch: false })
      }
    }
    window.addEventListener('nova:workspace-change', onWorkspaceChange)
    window.addEventListener(LORE_UPDATED_EVENT, onUpdated)
    return () => {
      window.removeEventListener(LORE_UPDATED_EVENT, onUpdated)
      window.removeEventListener('nova:workspace-change', onWorkspaceChange)
    }
  }, [client, projectId])
  const refreshRef = useRef(refreshSignal)
  useEffect(() => {
    if (refreshRef.current === refreshSignal) return
    refreshRef.current = refreshSignal
    void client.invalidateQueries({ queryKey: loreCategoriesKey(projectId) }, { cancelRefetch: false })
  }, [client, projectId, refreshSignal])
  const categories = query.data || DEFAULT_LORE_CATEGORIES
  const sections = useMemo(() => knowledgeSections(categories), [categories])
  return { ...query, categories, sections }
}
