import { useLayoutEffect, useState, type ReactNode } from 'react'
import { ChevronRight } from 'lucide-react'

import { Badge } from '@/components/ui/badge'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import { cn } from '@/lib/utils'
import type { SettingsFieldRequest } from './settings-sections'

type SettingsDisclosureLevel = 'connection' | 'model'

interface SettingsDisclosureCardProps {
  level: SettingsDisclosureLevel
  badge: string
  title: string
  subtitle: string
  defaultOpen: boolean
  revealRequest?: SettingsFieldRequest
  actions?: ReactNode
  children: ReactNode
}

export function SettingsDisclosureCard({ level, badge, title, subtitle, defaultOpen, revealRequest, actions, children }: SettingsDisclosureCardProps) {
  const isConnection = level === 'connection'
  const [open, setOpen] = useState(defaultOpen || Boolean(revealRequest))
  useLayoutEffect(() => {
    if (revealRequest) setOpen(true)
  }, [revealRequest])

  return (
    <Collapsible
      open={open}
      onOpenChange={setOpen}
      data-settings-level={level}
      className={cn(
        'overflow-hidden border bg-[var(--nova-surface-2)]',
        isConnection
          ? 'rounded-[var(--nova-radius)] border-[var(--nova-border)]'
          : 'rounded-md border-[var(--nova-border-soft)]',
      )}
    >
      <div className="flex min-w-0 items-stretch transition-colors hover:bg-[var(--nova-hover)]">
        <CollapsibleTrigger asChild>
          <button
            type="button"
            className={cn(
              'group/disclosure-trigger flex min-w-0 flex-1 items-center text-left outline-none focus-visible:ring-2 focus-visible:ring-ring/50',
              isConnection ? 'gap-2.5 rounded-l-[var(--nova-radius)] px-3 py-2.5' : 'gap-2 rounded-l-md px-2.5 py-2',
            )}
          >
            <ChevronRight className="size-4 shrink-0 text-[var(--nova-text-faint)] transition-transform group-data-[state=open]/disclosure-trigger:rotate-90" aria-hidden="true" />
            <Badge variant={isConnection ? 'secondary' : 'outline'} className="shrink-0">{badge}</Badge>
            <span className="min-w-0 flex-1">
              <span className={cn('block truncate text-xs text-[var(--nova-text)]', isConnection ? 'font-semibold' : 'font-medium')}>{title}</span>
              <span className="block truncate text-[11px] text-[var(--nova-text-faint)]">{subtitle}</span>
            </span>
          </button>
        </CollapsibleTrigger>
        {actions && (
          <div className="flex shrink-0 items-center px-1.5">
            {actions}
          </div>
        )}
      </div>
      <CollapsibleContent className={cn('border-t bg-[var(--nova-surface)]', isConnection ? 'border-[var(--nova-border)]' : 'border-[var(--nova-border-soft)]')}>
        {children}
      </CollapsibleContent>
    </Collapsible>
  )
}
