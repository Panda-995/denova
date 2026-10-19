import { jsonHeaders, requestJSON } from './client'
import type {
  LoreClassificationApplyRequest,
  LoreClassificationPreview,
  LoreClassificationPreviewRequest,
  LoreItem,
  LoreCategory,
  LoreIndexGuide,
  LoreIndexSnapshot,
  LoreIndexPreview,
  LoreAsset,
  LoreMaterialMutation,
  LoreItemImageGenerateRequest,
  LoreItemSpeechGenerateRequest,
  LoreTypeApplyResult,
} from './types'
import { projectFileAssetURL } from './project-files'
import { projectAPIPath } from './project-scope'

function lorePath(projectId: string, suffix: string): string {
  return projectAPIPath(projectId, `book/lore/${suffix.replace(/^\/+/, '')}`)
}

export function getLoreIndex(projectId: string): Promise<LoreIndexSnapshot> {
  return requestJSON(lorePath(projectId, 'index'))
}

export function updateLoreIndex(projectId: string, guide: LoreIndexGuide, baseRevision: string): Promise<LoreIndexSnapshot> {
  return requestJSON(lorePath(projectId, 'index'), { method: 'PUT', headers: jsonHeaders, body: JSON.stringify({ guide, base_revision: baseRevision }) })
}

export function previewLoreIndex(projectId: string, guide: LoreIndexGuide): Promise<LoreIndexPreview> {
  return requestJSON(lorePath(projectId, 'index/preview'), { method: 'POST', headers: jsonHeaders, body: JSON.stringify(guide) })
}

export function getLoreCategories(projectId: string): Promise<LoreCategory[]> {
  return requestJSON(lorePath(projectId, 'categories'))
}

export type LoreCategoryMutation =
  | { op: 'create'; name: string }
  | { op: 'rename'; id: string; name: string }
  | { op: 'delete'; id: string; destination_id: string }
  | { op: 'move'; id: string; index: number }

export function mutateLoreCategory(projectId: string, input: LoreCategoryMutation): Promise<LoreCategory[]> {
  return requestJSON(lorePath(projectId, 'categories'), { method: 'POST', headers: jsonHeaders, body: JSON.stringify(input) })
}

export async function previewLoreClassification(
  projectId: string,
  input: LoreClassificationPreviewRequest = {},
): Promise<LoreClassificationPreview> {
  return requestJSON(lorePath(projectId, 'classification/preview'), {
    method: 'POST',
    headers: jsonHeaders,
    body: JSON.stringify(input),
  })
}

export async function applyLoreClassification(
  projectId: string,
  input: LoreClassificationApplyRequest,
): Promise<LoreTypeApplyResult> {
  return requestJSON(lorePath(projectId, 'classification/apply'), {
    method: 'POST',
    headers: jsonHeaders,
    body: JSON.stringify(input),
  })
}

export async function generateLoreItemImage(
  projectId: string,
  id: string,
  input: LoreItemImageGenerateRequest = {},
): Promise<LoreItem> {
  return requestJSON(lorePath(projectId, `items/${encodeURIComponent(id)}/image/generate`), {
    method: 'POST',
    headers: jsonHeaders,
    body: JSON.stringify(input),
  })
}

export async function uploadLoreItemMaterial(
  projectId: string,
  id: string,
  file: File,
): Promise<LoreItem> {
  const form = new FormData()
  form.append('file', file, file.name)
  return requestJSON(lorePath(projectId, `items/${encodeURIComponent(id)}/materials/upload`), {
    method: 'POST',
    body: form,
  })
}

export function generateLoreItemSpeech(
  projectId: string,
  id: string,
  input: LoreItemSpeechGenerateRequest,
): Promise<LoreItem> {
  return requestJSON(lorePath(projectId, `items/${encodeURIComponent(id)}/speech/generate`), {
    method: 'POST',
    headers: jsonHeaders,
    body: JSON.stringify(input),
  })
}

export function getLoreAssets(projectId: string): Promise<LoreAsset[]> {
  return requestJSON(lorePath(projectId, 'assets'))
}
export function mutateLoreMaterial(
  projectId: string,
  id: string,
  mutation: LoreMaterialMutation,
): Promise<LoreItem> {
  return requestJSON(lorePath(projectId, `items/${encodeURIComponent(id)}/materials`), {
    method: 'POST',
    headers: jsonHeaders,
    body: JSON.stringify(mutation),
  })
}

/** Remote media bypasses the project file endpoint; URLs never become file paths. */
export function loreMaterialURL(projectId: string, asset: Pick<LoreAsset, 'path' | 'url'>): string {
  return asset.url || (asset.path ? projectFileAssetURL(projectId, asset.path) : '')
}

export function loreImageURL(projectId: string, item?: LoreItem): string {
  return item?.image?.image_url || (item?.image?.image_path ? projectFileAssetURL(projectId, item.image.image_path) : '')
}
