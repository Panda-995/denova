import type { ComponentProps, ReactNode } from 'react'
import { useControllableState } from '@radix-ui/react-use-controllable-state'
import { AnimatePresence, motion, useReducedMotionConfig } from 'motion/react'
import { Collapsible, CollapsibleContent } from '@/components/ui/collapsible'
import { novaEase } from '@/features/motion/motion-tokens'

/** Index disclosures share one height transition. Keep padding inside the animated
 * body so closing removes all space; exiting content immediately becomes inert. */
export function LoreIndexDisclosure({
  open, defaultOpen = false, onOpenChange, header, contentClassName, children, ...props
}: Omit<ComponentProps<typeof Collapsible>, 'asChild'> & {
  header: ReactNode
  contentClassName?: string
}) {
  const [expanded, setExpanded] = useControllableState({ prop: open, defaultProp: defaultOpen, onChange: onOpenChange })
  const reducedMotion = useReducedMotionConfig()
  return <Collapsible {...props} open={expanded} onOpenChange={setExpanded}>
    {header}
    <CollapsibleContent forceMount aria-hidden={!expanded} inert={!expanded}>
      {reducedMotion ? expanded && <div className={contentClassName}>{children}</div> : <AnimatePresence initial={false}>
        {expanded && <motion.div
          className="min-w-0 overflow-hidden"
          initial={{ height: 0, opacity: 0 }}
          animate={{ height: 'auto', opacity: 1 }}
          exit={{ height: 0, opacity: 0 }}
          transition={{ duration: 0.2, ease: novaEase }}
        >
          <div className={contentClassName}>{children}</div>
        </motion.div>}
      </AnimatePresence>}
    </CollapsibleContent>
  </Collapsible>
}
