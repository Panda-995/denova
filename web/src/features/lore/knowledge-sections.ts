import { Building2, Folder, Globe, MapPin, Package, UserRound, type LucideIcon } from 'lucide-react'
import type { LoreCategory, LoreItem } from '@/lib/api'

export type LoreType = string

export interface KnowledgeSection {
  id: string
  name?: string
  labelKey: string
  icon: LucideIcon
  createType: string
}

export const DEFAULT_LORE_CATEGORIES: LoreCategory[] = [
  { id: 'character' }, { id: 'location' }, { id: 'faction' }, { id: 'item' }, { id: 'world' },
]

const icons: Record<string, LucideIcon> = {
  character: UserRound, location: MapPin, faction: Building2, item: Package, world: Globe,
}

export function knowledgeSections(categories: LoreCategory[]): KnowledgeSection[] {
  return categories.map(({ id, name }) => ({
    id, name, labelKey: `lore.type.${id}`, icon: icons[id] || Folder,
    createType: id,
  }))
}

export function sectionItems(items: LoreItem[], section: KnowledgeSection) {
  return items.filter((item) => item.type === section.id)
}

export function firstVisibleLoreItemId(items: LoreItem[]): string | null {
  return items[0]?.id || null
}
