import { useEffect, useState, type RefObject } from 'react'
import { ArrowUp } from 'lucide-react'
import { useReducedMotionConfig } from 'motion/react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'

/** Mount beside the scroll container inside a positioned parent so the shortcut
 * stays in view and scrolls only its own pane. */
export function ScrollToTopButton({ scrollRef }: { scrollRef: RefObject<HTMLDivElement | null> }) {
  const { t } = useTranslation()
  const reducedMotion = useReducedMotionConfig()
  const [visible, setVisible] = useState(false)

  useEffect(() => {
    const container = scrollRef.current
    if (!container) return
    const update = () => setVisible(container.scrollTop > 320)
    update()
    container.addEventListener('scroll', update, { passive: true })
    return () => container.removeEventListener('scroll', update)
  }, [scrollRef])

  if (!visible) return null

  return <Button
    type="button" variant="outline" size="icon"
    className="absolute bottom-4 right-4 z-20 rounded-full shadow-md"
    aria-label={t('common.backToTop')} title={t('common.backToTop')}
    onClick={() => scrollRef.current?.scrollTo({ top: 0, behavior: reducedMotion ? 'instant' : 'smooth' })}
  ><ArrowUp /></Button>
}
