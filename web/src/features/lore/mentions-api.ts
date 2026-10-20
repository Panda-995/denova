import { requestJSON } from '@/lib/api-client/client'
import { assertProjectScope, projectAPIPath } from '@/lib/api-client/project-scope'
import type { BranchSummary, StoryIndex } from '@/features/interactive/types'

export interface NarrativeMention {
  turn_id: string
  timestamp: string
  source_revision: string
  matches: { line: number; preview: string }[]
  truncated: boolean
}

export interface NarrativeMentionPage {
  branch_id: string
  scanned_turns: number
  before_cursor?: string
  has_more: boolean
  hits: NarrativeMention[]
}

export interface NarrativeSource {
  turn_id: string
  timestamp: string
  narrative: string
}

/** Read projections are always bound to the Lore item's Project, not foreground navigation. */
async function projectStoryRead<T>(projectId: string, suffix: string, signal?: AbortSignal): Promise<T> {
  const result = await requestJSON<T & { project_id: string }>(projectAPIPath(projectId, `interactive/stories${suffix}`), { signal })
  assertProjectScope(projectId, result.project_id, 'Lore narrative response')
  return result
}

export function getMentionStories(projectId: string, signal?: AbortSignal) {
  return projectStoryRead<StoryIndex>(projectId, '', signal)
}

export async function getMentionBranches(projectId: string, storyId: string, signal?: AbortSignal) {
  return (await projectStoryRead<{ branches: BranchSummary[] }>(projectId, `/${encodeURIComponent(storyId)}/branches`, signal)).branches
}

export async function findNarrativeMentions(projectId: string, storyId: string, branch: string, query: string, before: string, signal?: AbortSignal) {
  const params = new URLSearchParams({ branch, q: query, before })
  return (await projectStoryRead<{ page: NarrativeMentionPage }>(projectId, `/${encodeURIComponent(storyId)}/mentions?${params}`, signal)).page
}

export async function readNarrativeSource(projectId: string, storyId: string, branch: string, turnId: string, revision: string, signal?: AbortSignal) {
  const params = new URLSearchParams({ branch, turn_id: turnId, revision })
  return (await projectStoryRead<{ source: NarrativeSource }>(projectId, `/${encodeURIComponent(storyId)}/narrative?${params}`, signal)).source
}
