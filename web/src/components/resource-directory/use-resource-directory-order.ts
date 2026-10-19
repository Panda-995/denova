import { useCallback, useMemo } from 'react'
import { useStore } from 'zustand'
import { createStore, type StoreApi } from 'zustand/vanilla'
import type { ResourceDirectorySection } from './types'

type DirectoryOrder = Record<string, string[]>
const ORDER_VERSION = 1
const orderStores = new Map<string, StoreApi<DirectoryOrder>>()

/** Local display preference shared by directories and overviews using the same storage key.
 * Each section key identifies a sortable set of items or groups. Project preferences
 * must include the stable Project ID in the storage key. */
export function useResourceDirectoryOrder(storageKey: string) {
  const store = useMemo(() => {
    let existing = orderStores.get(storageKey)
    if (!existing) {
      existing = createStore<DirectoryOrder>(() => readDirectoryOrder(storageKey))
      orderStores.set(storageKey, existing)
    }
    return existing
  }, [storageKey])
  const order = useStore(store)
  const reorderItems = useCallback((sectionId: string, visibleItemIds: string[], allItemIds: string[]) => {
    const current = store.getState()
    const canonical = orderKnownIDs(allItemIds, current[sectionId])
    const visible = new Set(visibleItemIds)
    let position = 0
    const next = { ...current, [sectionId]: canonical.map(id => visible.has(id) ? visibleItemIds[position++] : id) }
    // Commit synchronously with drag end so rows never render their old order after drop.
    store.setState(next, true)
    try {
      window.localStorage.setItem(storageKey, JSON.stringify({ version: ORDER_VERSION, sections: next }))
    } catch (error) {
      console.warn('[resource-directory] failed to save directory order', { storageKey, error })
    }
    console.info('[resource-directory] item order changed', { storageKey, sectionId, items: visibleItemIds.length })
  }, [store, storageKey])

  return { order, reorderItems }
}

/** Saved entries keep their order; new entries follow the data source order. */
export function applyResourceDirectoryOrder(sections: ResourceDirectorySection[], order: DirectoryOrder): ResourceDirectorySection[] {
  return sections.map(section => {
    const items = new Map(section.items.map(item => [item.id, item]))
    return { ...section, items: orderKnownIDs([...items.keys()], order[section.id]).map(id => items.get(id)!) }
  })
}

function orderKnownIDs(ids: string[], storedOrder: string[] = []): string[] {
  const known = new Set(ids)
  const ordered = storedOrder.filter(id => known.has(id))
  const orderedSet = new Set(ordered)
  return [...ordered, ...ids.filter(id => !orderedSet.has(id))]
}

function readDirectoryOrder(storageKey: string): DirectoryOrder {
  try {
    const raw = window.localStorage.getItem(storageKey)
    if (!raw) return {}
    const parsed = JSON.parse(raw) as { version?: unknown; sections?: unknown }
    if (parsed.version !== ORDER_VERSION || !parsed.sections || typeof parsed.sections !== 'object') return {}
    return Object.fromEntries(Object.entries(parsed.sections).flatMap(([sectionId, value]) => (
      Array.isArray(value) ? [[sectionId, [...new Set(value.filter((id): id is string => typeof id === 'string' && id.length > 0))]]] : []
    )))
  } catch (error) {
    console.warn('[resource-directory] failed to read directory order', { storageKey, error })
    return {}
  }
}
