import type { KeyboardEvent, MouseEvent, ReactNode, TouchEvent } from 'react'
import { DndContext, KeyboardSensor, MouseSensor, TouchSensor, closestCenter, pointerWithin, useSensor, useSensors } from '@dnd-kit/core'
import { SortableContext, arrayMove, rectSortingStrategy, sortableKeyboardCoordinates, useSortable, verticalListSortingStrategy } from '@dnd-kit/sortable'
import { CSS } from '@dnd-kit/utilities'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'

/** Saved positions only affect present entries. Missing entries retain their
 * place in storage; newly discovered entries follow the backend's default order. */
export function orderIndexEntries<T>(entries: T[], order: string[] | undefined, key: (entry: T) => string): T[] {
  const ranks = new Map(order?.map((id, position) => [id, position]))
  return [...entries].sort((a, b) => (ranks.get(key(a)) ?? ranks.size) - (ranks.get(key(b)) ?? ranks.size))
}

/** Replace visible slots while preserving positions of temporarily hidden entries. */
export function mergeIndexOrder(saved: string[] | undefined, visible: string[]): string[] {
  const visibleIDs = new Set(visible)
  let position = 0
  const order = (saved ?? []).map(id => visibleIDs.has(id) ? visible[position++] : id)
  return [...order, ...visible.slice(position)]
}

// Each group has an independent nested context, so dragging a card never moves
// its section or changes membership. Mouse movement and touch hold distinguish
// dragging from clicking and scrolling without adding a handle to the card.
export function LoreIndexSortArea({ ids, layout, onReorder, children }: {
  ids: string[]; layout: 'list' | 'grid'; onReorder: (ids: string[]) => void; children: ReactNode
}) {
  const { t } = useTranslation()
  const sensors = useSensors(
    useSensor(MouseSensor, { activationConstraint: { distance: 8 } }),
    useSensor(TouchSensor, { activationConstraint: { delay: 220, tolerance: 8 } }),
    useSensor(KeyboardSensor, {
      keyboardCodes: { start: ['Space'], cancel: ['Escape'], end: ['Space', 'Enter'] },
      coordinateGetter: (event, args) => {
        if (layout === 'grid') return sortableKeyboardCoordinates(event, args)
        const { active, over, collisionRect, droppableRects } = args.context
        if (!active || !collisionRect || (event.code !== 'ArrowUp' && event.code !== 'ArrowDown')) return
        event.preventDefault()
        const position = ids.indexOf(String(over?.id ?? active.id)) + (event.code === 'ArrowUp' ? -1 : 1)
        const target = droppableRects.get(ids[position])
        // Center alignment keeps one keypress equal to one section, even when
        // an expanded section is much taller than its collapsed neighbor.
        if (target) return { x: target.left, y: target.top + (target.height - collisionRect.height) / 2 }
      },
    }),
  )
  return <DndContext sensors={sensors} collisionDetection={args => {
    // Expanded groups have different heights. Follow the pointer's destination
    // rather than the dragged section's center, which may be far below its title.
    if (layout === 'list' && args.pointerCoordinates) return pointerWithin({ ...args, droppableContainers: args.droppableContainers.filter(container => container.id !== args.active.id) })
    return closestCenter(args)
  }}
    accessibility={{
      screenReaderInstructions: { draggable: t('lore.index.dragInstructions') },
      announcements: {
        onDragStart: () => t('lore.index.dragStarted'),
        onDragOver: ({ over }) => over ? t('lore.index.dragPosition', { position: ids.indexOf(String(over.id)) + 1, count: ids.length }) : undefined,
        onDragEnd: () => t('lore.index.dragFinished'),
        onDragCancel: () => t('lore.index.dragCancelled'),
      },
    }}
    onDragEnd={({ active, over }) => {
      if (!over || active.id === over.id) return
      const from = ids.indexOf(String(active.id)), to = ids.indexOf(String(over.id))
      if (from >= 0 && to >= 0) onReorder(arrayMove(ids, from, to))
    }}
  >
    <SortableContext items={ids} strategy={layout === 'grid' ? rectSortingStrategy : verticalListSortingStrategy}>{children}</SortableContext>
  </DndContext>
}

/** Bind the activator to the existing header button or complete card. */
export function useIndexSortable(id: string, disabled: boolean) {
  const { t } = useTranslation()
  const { attributes, listeners, setNodeRef, setActivatorNodeRef, transform, transition, isDragging } = useSortable({ id, disabled })
  return {
    containerProps: {
      ref: setNodeRef,
      style: { transform: CSS.Transform.toString(transform), transition, position: 'relative' as const, zIndex: isDragging ? 10 : undefined, opacity: isDragging ? 0.7 : undefined },
    },
    activatorProps: {
      ...attributes,
      ref: setActivatorNodeRef,
      'aria-roledescription': t('lore.index.sortable'),
      onMouseDown: (event: MouseEvent<HTMLElement>) => listeners?.onMouseDown?.(event),
      onTouchStart: (event: TouchEvent<HTMLElement>) => listeners?.onTouchStart?.(event),
      onKeyDown: (event: KeyboardEvent<HTMLElement>) => listeners?.onKeyDown?.(event),
    },
  }
}

export function LoreIndexSortableCard({ id, name, disabled, children }: { id: string; name: string; disabled: boolean; children: ReactNode }) {
  const { t } = useTranslation()
  const { containerProps, activatorProps } = useIndexSortable(id, disabled)
  const { ref: activatorRef, onMouseDown, onTouchStart, onKeyDown, ...attributes } = activatorProps
  // Disabling sorting must not mark the card's selection and loading controls
  // disabled through their ancestor. Each control owns its busy state.
  return <div {...containerProps} {...attributes}
    ref={node => { containerProps.ref(node); activatorRef(node) }}
    role={disabled ? undefined : attributes.role}
    tabIndex={disabled ? undefined : attributes.tabIndex}
    aria-disabled={undefined}
    aria-roledescription={disabled ? undefined : attributes['aria-roledescription']}
    aria-describedby={disabled ? undefined : attributes['aria-describedby']}
    aria-label={t('lore.index.sortItem', { name })}
    className={cn('min-w-0 rounded-lg outline-none focus-visible:ring-2 focus-visible:ring-ring [&>[data-slot=card]]:h-full', !disabled && '[&>[data-slot=card]]:cursor-grab [&>[data-slot=card]]:active:cursor-grabbing')}
    onMouseDown={event => { if (!(event.target as Element).closest('[data-index-control]')) onMouseDown?.(event) }}
    onTouchStart={event => { if (!(event.target as Element).closest('[data-index-control]')) onTouchStart?.(event) }}
    onKeyDown={event => { if (event.target === event.currentTarget) onKeyDown?.(event) }}
    onDragStart={event => event.preventDefault()}
  >{children}</div>
}
