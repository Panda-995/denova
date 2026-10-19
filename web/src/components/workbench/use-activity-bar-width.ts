import { useEffect, useRef, useState, type KeyboardEventHandler, type PointerEventHandler } from 'react'

// User-level width preference; it survives reloads and development hot updates.
const STORAGE_KEY = 'nova.layout.activityBarWidth'
const MIN_WIDTH = 192
const LEGACY_DEFAULT_WIDTH = 152
const DEFAULT_WIDTH = 224
const MAX_WIDTH = 288
const KEYBOARD_STEP = 8

/** Owns primary-menu sizing. Pointer previews update the DOM; only completed resizes update React and storage. */
export function useActivityBarWidth(enabled: boolean) {
  const [width, setWidth] = useState(readStoredWidth)
  const wrapperRef = useRef<HTMLDivElement>(null)
  const finishResizeRef = useRef<(() => void) | null>(null)

  useEffect(() => () => finishResizeRef.current?.(), [enabled])

  const commitWidth = (nextWidth: number) => {
    setWidth(nextWidth)
    window.localStorage.setItem(STORAGE_KEY, String(nextWidth))
  }

  const onResizePointerDown: PointerEventHandler<HTMLDivElement> = (event) => {
    const wrapper = wrapperRef.current
    const handle = event.currentTarget
    const sidebar = handle.closest<HTMLElement>('[data-slot="sidebar"]')
    if (!enabled || !wrapper || !sidebar || event.button !== 0 || !event.isPrimary || finishResizeRef.current) return

    event.preventDefault()
    const pointerID = event.pointerId
    const startX = event.clientX
    let previewWidth = width
    // Disable both the visible sidebar and its layout gap before the first move.
    sidebar.setAttribute('data-nova-activity-bar-resizing', 'true')
    handle.setPointerCapture(pointerID)

    const move = (moveEvent: PointerEvent) => {
      if (moveEvent.pointerId !== pointerID) return
      const nextWidth = clampWidth(width + moveEvent.clientX - startX)
      if (nextWidth === previewWidth) return
      previewWidth = nextWidth
      wrapper.style.setProperty('--sidebar-width', `${nextWidth}px`)
      handle.setAttribute('aria-valuenow', String(nextWidth))
    }
    const finish = () => {
      finishResizeRef.current = null
      window.removeEventListener('pointermove', move)
      window.removeEventListener('pointerup', end)
      window.removeEventListener('pointercancel', cancel)
      window.removeEventListener('blur', finish)
      handle.removeEventListener('lostpointercapture', finish)
      // Apply the final pointer coordinate before restoring collapse/expand transitions.
      handle.getBoundingClientRect()
      sidebar.removeAttribute('data-nova-activity-bar-resizing')
      if (handle.hasPointerCapture(pointerID)) handle.releasePointerCapture(pointerID)
      if (previewWidth !== width) commitWidth(previewWidth)
    }
    const end = (endEvent: PointerEvent) => {
      if (endEvent.pointerId !== pointerID) return
      move(endEvent)
      finish()
    }
    const cancel = (cancelEvent: PointerEvent) => {
      if (cancelEvent.pointerId === pointerID) finish()
    }
    finishResizeRef.current = finish
    window.addEventListener('pointermove', move)
    window.addEventListener('pointerup', end)
    window.addEventListener('pointercancel', cancel)
    window.addEventListener('blur', finish)
    handle.addEventListener('lostpointercapture', finish)
  }

  const onResizeKeyDown: KeyboardEventHandler<HTMLDivElement> = (event) => {
    if (!enabled || finishResizeRef.current) return
    let nextWidth: number
    switch (event.key) {
      case 'ArrowLeft': nextWidth = clampWidth(width - KEYBOARD_STEP); break
      case 'ArrowRight': nextWidth = clampWidth(width + KEYBOARD_STEP); break
      case 'Home': nextWidth = MIN_WIDTH; break
      case 'End': nextWidth = MAX_WIDTH; break
      default: return
    }
    event.preventDefault()
    if (nextWidth !== width) commitWidth(nextWidth)
  }

  return { width, minWidth: MIN_WIDTH, maxWidth: MAX_WIDTH, wrapperRef, onResizePointerDown, onResizeKeyDown }
}

function readStoredWidth() {
  if (typeof window === 'undefined') return DEFAULT_WIDTH
  const raw = window.localStorage.getItem(STORAGE_KEY)
  if (raw === null) return DEFAULT_WIDTH
  const value = Number(raw)
  if (value === LEGACY_DEFAULT_WIDTH) return DEFAULT_WIDTH
  return Number.isFinite(value) ? clampWidth(value) : DEFAULT_WIDTH
}

function clampWidth(width: number) {
  return Math.min(MAX_WIDTH, Math.max(MIN_WIDTH, Math.round(width)))
}
