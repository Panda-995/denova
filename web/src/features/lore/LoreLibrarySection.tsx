import type { ReactNode } from 'react'
import { ChevronDown, type LucideIcon } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import { Separator } from '@/components/ui/separator'
import { cn } from '@/lib/utils'
import { LORE_CARD_LAYOUTS, type LoreCardSize } from './LoreCard'
import { LoreIndexSortArea, useIndexSortable } from './LoreIndexSorting'

/** Overview sections reuse index sorting; callbacks only change display order. */
export function LoreLibrarySection({ id, name, icon: Icon, ids, cardSize, disabled, open, onOpenChange, onReorder, children }: {
  id: string
  name: string
  icon: LucideIcon
  ids: string[]
  cardSize: LoreCardSize
  disabled: boolean
  open: boolean
  onOpenChange: (open: boolean) => void
  onReorder: (ids: string[]) => void
  children: ReactNode
}) {
  const { containerProps, activatorProps } = useIndexSortable(id, disabled)
  return (
    <Collapsible asChild open={open} onOpenChange={onOpenChange}>
      <section {...containerProps} aria-label={name} className="min-w-0">
        <h2>
          <CollapsibleTrigger asChild>
            <Button {...activatorProps} aria-disabled={undefined}
              aria-roledescription={disabled ? undefined : activatorProps['aria-roledescription']}
              aria-describedby={disabled ? undefined : activatorProps['aria-describedby']}
              type="button" variant="ghost" className={cn(
              'h-auto w-full justify-start gap-2 py-2 text-left whitespace-normal aria-expanded:bg-transparent',
              !disabled && 'cursor-grab active:cursor-grabbing',
            )}>
              <ChevronDown className={cn('transition-transform', !open && '-rotate-90')} aria-hidden="true" />
              <Icon aria-hidden="true" />
              <span className="min-w-0 break-all">{name}</span>
              <span className="text-xs text-muted-foreground">{ids.length}</span>
              <Separator className="ml-2 min-w-0 flex-1" />
            </Button>
          </CollapsibleTrigger>
        </h2>
        <CollapsibleContent>
          <LoreIndexSortArea ids={ids} layout="grid" onReorder={onReorder}>
            <div className={cn('grid pt-4', LORE_CARD_LAYOUTS[cardSize])}>{children}</div>
          </LoreIndexSortArea>
        </CollapsibleContent>
      </section>
    </Collapsible>
  )
}
