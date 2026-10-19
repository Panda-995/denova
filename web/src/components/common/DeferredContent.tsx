import { useEffect, useLayoutEffect, useRef, useState, type CSSProperties, type ReactNode } from 'react'

const pending = new Map<Element, () => void>()
let observer: IntersectionObserver | undefined

/** Delay expensive offscreen controls, retaining them once visited so scrolling
 * cannot interrupt edits, open menus, or in-flight mutations. The outer slot
 * remains measurable for keyboard navigation and drag-and-drop. */
export function DeferredContent({ children, placeholder, style, className, eager = false }: {
  children: ReactNode
  placeholder: ReactNode
  style?: CSSProperties
  className?: string
  eager?: boolean
}) {
  const ref = useRef<HTMLDivElement>(null)
  const focusPending = useRef(false)
  const [mounted, setMounted] = useState(eager)
  useLayoutEffect(() => {
    if (eager) setMounted(true)
    if (mounted && focusPending.current) {
      focusPending.current = false
      ref.current?.querySelector<HTMLElement>('button, [tabindex="0"]')?.focus({ preventScroll: true })
    }
  }, [eager, mounted])
  useEffect(() => {
    if (mounted || !ref.current) return
    if (!('IntersectionObserver' in window)) {
      setMounted(true)
      return
    }
    observer ??= new IntersectionObserver((entries, subscription) => {
      for (const entry of entries) {
        if (!entry.isIntersecting) continue
        pending.get(entry.target)?.()
        pending.delete(entry.target)
        subscription.unobserve(entry.target)
      }
    }, { rootMargin: '400px' })
    const element = ref.current
    const subscription = observer
    pending.set(element, () => setMounted(true))
    observer.observe(element)
    return () => {
      pending.delete(element)
      subscription.unobserve(element)
      if (!pending.size && observer === subscription) {
        subscription.disconnect()
        observer = undefined
      }
    }
  }, [mounted])
  return <div ref={ref} className={className} style={style} onFocusCapture={event => {
    if (!mounted && event.target.matches(':focus-visible')) {
      focusPending.current = true
      setMounted(true)
    }
  }}>
    {mounted ? children : placeholder}
  </div>
}
