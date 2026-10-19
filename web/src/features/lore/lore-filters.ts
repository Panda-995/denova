import { loreImageURL, type LoreItem } from '@/lib/api'
import { isLoreProtagonistTag, LORE_PROTAGONIST_TAG } from './tags'

/** Transient browsing conditions. A page owns one value for its directory and overview. */
export interface LoreFilters {
  category: string
  tags: 'all' | 'untagged' | string[]
  loadMode: 'all' | LoreItem['load_mode']
  enabled: 'all' | 'enabled' | 'disabled'
  importance: 'all' | LoreItem['importance']
  cover: 'all' | 'with' | 'without'
}

export const EMPTY_LORE_FILTERS: LoreFilters = {
  category: 'all',
  tags: 'all',
  loadMode: 'all',
  enabled: 'all',
  importance: 'all',
  cover: 'all',
}

/** Keep the existing bilingual protagonist aliases together without rewriting user tags. */
export function loreTagFilterValue(tag: string): string {
  return isLoreProtagonistTag(tag) ? LORE_PROTAGONIST_TAG : tag
}

export function filterLoreItems(
  items: LoreItem[],
  filters: LoreFilters,
  query: string,
  projectId: string,
): LoreItem[] {
  const candidates = filters.category === 'all' ? items : items.filter((item) => item.type === filters.category)
  const words = query.trim().toLocaleLowerCase().split(/\s+/)
  return candidates.filter((item) => {
    const tags = item.tags || []
    if (filters.tags === 'untagged' && tags.length > 0) return false
    if (Array.isArray(filters.tags) && !tags.some((tag) => filters.tags.includes(loreTagFilterValue(tag)))) return false
    if (filters.loadMode !== 'all' && (item.load_mode || 'auto') !== filters.loadMode) return false
    if (filters.enabled !== 'all' && (item.enabled !== false) !== (filters.enabled === 'enabled')) return false
    if (filters.importance !== 'all' && item.importance !== filters.importance) return false
    if (filters.cover !== 'all' && Boolean(loreImageURL(projectId, item)) !== (filters.cover === 'with')) return false
    const text = [item.name, item.brief_description, item.content, ...tags, ...(item.keywords || [])]
      .join('\n').toLocaleLowerCase()
    return words.every((word) => text.includes(word))
  })
}
