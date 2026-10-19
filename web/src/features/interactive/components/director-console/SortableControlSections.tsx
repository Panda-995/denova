import { Children, createContext, isValidElement, useContext, useState, type ReactNode } from 'react'
import { DndContext, KeyboardSensor, PointerSensor, closestCenter, pointerWithin, rectIntersection, useSensor, useSensors, type CollisionDetection } from '@dnd-kit/core'
import { SortableContext, arrayMove, sortableKeyboardCoordinates, useSortable, verticalListSortingStrategy } from '@dnd-kit/sortable'
import { CSS } from '@dnd-kit/utilities'
import { useTranslation } from 'react-i18next'

/** Pointer sorting follows the title under the cursor, independent of expanded card height.
 * Keyboard sorting uses overlap so a short card can enter a tall card without reaching its center.
 */
export const controlSectionCollision: CollisionDetection = (args) => {
  const hits = args.pointerCoordinates ? pointerWithin(args) : rectIntersection(args)
  return hits.length ? hits : closestCenter(args)
}

const STORAGE_KEY = 'nova.interactive.controlSections.v1'
interface Preferences { order: string[]; collapsed: string[] }

function readPreferences(): Preferences {
  try {
    const value = JSON.parse(localStorage.getItem(STORAGE_KEY) || '{}')
    return {
      order: Array.isArray(value?.order) ? value.order.filter((id: unknown) => typeof id === 'string') : [],
      collapsed: Array.isArray(value?.collapsed) ? value.collapsed.filter((id: unknown) => typeof id === 'string') : [],
    }
  } catch (error) {
    console.warn('[game-console] Failed to read control layout', error)
    return { order: [], collapsed: [] }
  }
}

const SectionContext = createContext<{
  expanded: boolean
  toggle: () => void
  sortable: ReturnType<typeof useSortable>
} | null>(null)

export const useControlSectionLayout = () => useContext(SectionContext)

/** Local display preferences shared across stories. Child keys are stable section identities. */
export function SortableControlSections({ children }: { children: ReactNode }) {
  const { t } = useTranslation()
  const [preferences, setPreferences] = useState(readPreferences)
  const sections = new Map<string, ReactNode>()
  Children.forEach(children, child => {
    if (isValidElement(child) && child.key != null) sections.set(String(child.key), child)
  })
  const order = [...new Set([...preferences.order.filter(id => sections.has(id)), ...sections.keys()])]
  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 6 } }),
    // Enter toggles the section; Space starts keyboard sorting.
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates, keyboardCodes: { start: ['Space'], cancel: ['Escape'], end: ['Space', 'Enter'] } }),
  )
  const save = (next: Preferences) => {
    setPreferences(next)
    try { localStorage.setItem(STORAGE_KEY, JSON.stringify(next)) } catch (error) {
      console.warn('[game-console] Failed to save control layout', error)
    }
  }
  return (
    <DndContext sensors={sensors} collisionDetection={controlSectionCollision}
      accessibility={{ screenReaderInstructions: { draggable: t('directorPanel.tuning.sortInstructions') }, announcements: {
        onDragStart: () => t('directorPanel.tuning.sortStarted'),
        onDragOver: ({ over }) => over ? t('directorPanel.tuning.sortPosition', { position: order.indexOf(String(over.id)) + 1 }) : undefined,
        onDragEnd: () => t('directorPanel.tuning.sortFinished'),
        onDragCancel: () => t('directorPanel.tuning.sortCancelled'),
      } }}
      onDragEnd={({ active, over }) => {
        if (!over || active.id === over.id) return
        const from = order.indexOf(String(active.id)), to = order.indexOf(String(over.id))
        if (from >= 0 && to >= 0) save({ ...preferences, order: arrayMove(order, from, to) })
      }}>
      <SortableContext items={order} strategy={verticalListSortingStrategy}>
        <div className="flex flex-col gap-2">
          {order.map(id => <SortableSection key={id} id={id} expanded={!preferences.collapsed.includes(id)}
            toggle={() => save({ order, collapsed: preferences.collapsed.includes(id) ? preferences.collapsed.filter(key => key !== id) : [...preferences.collapsed, id] })}>
            {sections.get(id)}
          </SortableSection>)}
        </div>
      </SortableContext>
    </DndContext>
  )
}

function SortableSection({ id, expanded, toggle, children }: { id: string; expanded: boolean; toggle: () => void; children: ReactNode }) {
  const sortable = useSortable({ id })
  // Cards have different heights; sorting must not scale them to the drop target.
  return (
    <div ref={sortable.setNodeRef} data-control-section={id}
      className={sortable.isDragging ? 'relative z-10 shadow-lg' : undefined}
      style={{ transform: CSS.Translate.toString(sortable.transform), transition: sortable.transition }}>
      <SectionContext.Provider value={{ expanded, toggle, sortable }}>{children}</SectionContext.Provider>
    </div>
  )
}
