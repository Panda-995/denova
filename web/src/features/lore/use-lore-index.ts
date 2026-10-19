import { useEffect } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { getLoreIndex } from '@/lib/api-client/lore'
import { workspaceChangePaths, type WorkspaceChangeEvent } from '@/features/changes/types'
import { isLoreItemsPath } from '@/lib/workspace-path'
import { LORE_UPDATED_EVENT, type LoreUpdatedDetail } from './events'

export const loreIndexKey = (projectId: string) => ['lore-index', projectId] as const

/** Read projection shared by the document and the individual item editors. */
export function useLoreIndex(projectId: string) {
  const client = useQueryClient()
  const query = useQuery({ queryKey: loreIndexKey(projectId), queryFn: () => getLoreIndex(projectId), enabled: Boolean(projectId), staleTime: 30_000 })
  useEffect(() => {
    const refresh = () => { void client.invalidateQueries({ queryKey: loreIndexKey(projectId) }) }
    const onUpdated = (event: Event) => {
      if ((event as CustomEvent<LoreUpdatedDetail>).detail?.projectId === projectId) refresh()
    }
    const onWorkspaceChange = (event: Event) => {
      const detail = (event as CustomEvent<WorkspaceChangeEvent>).detail
      if (detail?.project_id === projectId && (detail.resync || workspaceChangePaths(detail).some(isLoreItemsPath))) refresh()
    }
    window.addEventListener(LORE_UPDATED_EVENT, onUpdated)
    window.addEventListener('nova:workspace-change', onWorkspaceChange)
    return () => {
      window.removeEventListener(LORE_UPDATED_EVENT, onUpdated)
      window.removeEventListener('nova:workspace-change', onWorkspaceChange)
    }
  }, [client, projectId])
  return query
}
