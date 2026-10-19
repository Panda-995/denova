import { useLayoutEffect, useRef, useState, type ReactNode } from 'react'
import { useGroupRef, type Layout } from 'react-resizable-panels'
import type { AdaptiveSurfacePane } from './adaptive-surface'
import { panelSizeInPixels, resolveAdaptivePanelLayout, type AdaptivePanelSizing } from './adaptive-panel-layout'
import { CollapsiblePanelSeparator, PanelMotionGroup, PanelMotionPanel } from './panel-motion'
import { usePersistedPanelLayout } from './use-persisted-panel-layout'

/** One horizontal group owns all three panes; no nested ResizeObserver can fight its toggle. */
export function AdaptiveResizablePanes({ left, right, sizing, children }: {
  left: AdaptiveSurfacePane
  right: AdaptiveSurfacePane | null
  sizing: AdaptivePanelSizing
  children: ReactNode
}) {
  const groupRef = useGroupRef()
  const elementRef = useRef<HTMLDivElement>(null)
  const applyingLayout = useRef(false)
  const [width, setWidth] = useState(0)
  const [leftPixels, setLeftPixels] = useState<number | null>(null)
  const previousWidth = useRef(0)
  const leftLayout = usePersistedPanelLayout({ storageKey: sizing.left.layoutKey, panelIds: ['left', 'main'] })
  const rightLayout = usePersistedPanelLayout({ storageKey: sizing.right.layoutKey, panelIds: ['main', 'right'] })
  // These are user preferences, not snapshots of constrained/animated panel widths. The two
  // existing storage scopes remain independent: directory globally, content split per project.
  const preferredRight = useRef<{ key: string; ratio: number | null }>({ key: sizing.right.layoutKey, ratio: null })
  if (preferredRight.current.key !== sizing.right.layoutKey) {
    preferredRight.current = { key: sizing.right.layoutKey, ratio: null }
  }

  function initialLeftPixels(width: number) {
    return leftLayout.defaultLayout?.left
      ? width * leftLayout.defaultLayout.left / 100
      : panelSizeInPixels(sizing.left.defaultSize ?? '288px', width)
  }

  function resolve(width: number, navigationPixels = leftPixels ?? initialLeftPixels(width)) {
    if (width <= 0) return null
    const contentWidth = width - (sizing.leftVisible ? navigationPixels : 0)
    preferredRight.current.ratio ??= rightLayout.defaultLayout?.right
      ? rightLayout.defaultLayout.right / 100
      : panelSizeInPixels(sizing.right.defaultSize ?? '420px', contentWidth) / contentWidth
    return resolveAdaptivePanelLayout(sizing, width, navigationPixels, preferredRight.current.ratio)
  }

  const resolved = resolve(width)

  function applyLayout(navigationPixels?: number) {
    if (applyingLayout.current || !groupRef.current) return
    const element = elementRef.current
    const measuredWidth = element?.clientWidth ?? 0
    const target = resolve(measuredWidth, navigationPixels)
    if (!target) return
    if (measuredWidth !== previousWidth.current) element?.setAttribute('data-nova-panel-motion-suspended', 'true')
    applyingLayout.current = true
    try {
      groupRef.current.setLayout(target.layout)
    } finally {
      applyingLayout.current = false
    }
  }

  function onLayoutChange(layout: Layout) {
    if (applyingLayout.current) return
    const measuredWidth = elementRef.current?.clientWidth ?? 0
    let navigationPixels: number | undefined
    if (sizing.leftVisible && leftLayout.isUserResizeActive()) {
      navigationPixels = layout.left / 100 * measuredWidth
      setLeftPixels(navigationPixels)
      leftLayout.persistUserLayout({ left: layout.left, main: 100 - layout.left })
    } else if (sizing.rightVisible && !sizing.rightExpanded && rightLayout.isUserResizeActive()) {
      preferredRight.current.ratio = layout.right / (layout.main + layout.right)
      rightLayout.persistUserLayout({ main: 100 * (1 - preferredRight.current.ratio), right: 100 * preferredRight.current.ratio })
    }
    // Registration, container resize and user drag all converge on the same constrained target.
    // setLayout updates every sibling atomically. The guard ignores its synchronous callback.
    applyLayout(navigationPixels)
  }

  useLayoutEffect(() => {
    const element = elementRef.current
    if (!element) return
    const measure = () => {
      if (element.clientWidth > 0) {
        setWidth(element.clientWidth)
        setLeftPixels(current => current ?? initialLeftPixels(element.clientWidth))
      }
    }
    measure()
    let frame = 0
    const observer = new ResizeObserver(() => {
      cancelAnimationFrame(frame)
      frame = requestAnimationFrame(measure)
    })
    observer.observe(element)
    return () => { observer.disconnect(); cancelAnimationFrame(frame) }
  }, [])

  useLayoutEffect(() => {
    const element = elementRef.current
    if (!element || width <= 0) return
    const passiveResize = previousWidth.current !== width
    if (passiveResize) element.setAttribute('data-nova-panel-motion-suspended', 'true')
    applyLayout()
    if (passiveResize) {
      // Flush the immediate correction before re-enabling motion for the next explicit toggle.
      element.getBoundingClientRect()
      element.removeAttribute('data-nova-panel-motion-suspended')
    }
    previousWidth.current = width
  })

  const minimum = resolved?.minimum ?? { left: 0, main: 0, right: 0 }
  // Before measurement the library still needs a feasible layout. Three zero maxima produce a
  // zero-total layout that cannot be normalized when a drawer returns to its desktop group.
  const maximum = resolved?.maximum ?? { left: '100%', main: '100%', right: '100%' }
  const separatorClassName = 'nova-resize-handle nova-resize-divider nova-resize-divider-vertical relative z-30 -mx-1 w-2 shrink-0 touch-none cursor-col-resize select-none'
  return (
    <PanelMotionGroup
      id={sizing.left.layoutKey}
      data-nova-content-layout-key={sizing.right.layoutKey}
      elementRef={elementRef}
      groupRef={groupRef}
      orientation="horizontal"
      resizeTargetMinimumSize={{ coarse: 16, fine: 1 }}
      onLayoutChange={onLayoutChange}
      className="h-full min-h-0 min-w-0 flex-1"
    >
      <PanelMotionPanel id="left" visible={sizing.leftVisible} side="left"
        minSize={minimum.left} maxSize={maximum.left} disabled={!sizing.leftVisible}
        className={`min-w-0 ${left.desktopClassName ?? ''}`}>
        {left.content}
      </PanelMotionPanel>
      <CollapsiblePanelSeparator visible={sizing.leftVisible} aria-label={sizing.left.label}
        className={separatorClassName} {...leftLayout.resizeHandleIntentProps} />
      <PanelMotionPanel id="main" visible={!sizing.rightExpanded} side="left"
        minSize={minimum.main} maxSize={maximum.main} disabled={sizing.rightExpanded} className="min-w-0">
        {children}
      </PanelMotionPanel>
      <CollapsiblePanelSeparator visible={sizing.rightVisible && !sizing.rightExpanded} aria-label={sizing.right.label}
        className={separatorClassName} {...rightLayout.resizeHandleIntentProps} />
      <PanelMotionPanel id="right" visible={sizing.rightVisible} side="right"
        minSize={minimum.right} maxSize={maximum.right} disabled={!sizing.rightVisible}
        className={`min-w-0 ${right?.desktopClassName ?? ''}`}>
        {right?.content}
      </PanelMotionPanel>
    </PanelMotionGroup>
  )
}
