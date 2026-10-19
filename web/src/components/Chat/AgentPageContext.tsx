import type { AgentPageContext } from '@/lib/api-client/types'
import { ImageIcon, Paperclip, type LucideIcon } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'

/** Host-owned page accessories for the shared composer. Actions stay in the
 * page; suggestions fill the composer and never submit a turn themselves. */
export interface AgentPage {
  context: AgentPageContext
  label?: string
  icon?: LucideIcon
  referenceCount?: number
  suggestions?: { label: string; prompt: string }[]
  actions?: { id: string; label: string }[]
  onAction: (id: string) => void
}

export function AgentPageAccessories({ page, onPrefill }: { page: AgentPage; onPrefill: (prompt: string) => void }) {
  const Icon = page.icon ?? ImageIcon
  return (
    <div className="pointer-events-auto mb-2 flex min-w-0 flex-col gap-2" data-agent-page={page.context.page_id}>
      {page.label && <Badge variant="secondary" className="max-w-full self-start">
        <Icon /><span className="truncate">{page.label}</span>
      </Badge>}
      <div className="flex flex-wrap gap-1">
        {page.actions?.map((action) => <Button key={action.id} type="button" size="xs" variant="outline" onClick={() => page.onAction(action.id)}>
          <Paperclip />{action.label}{page.referenceCount ? ` ${page.referenceCount}` : ''}
        </Button>)}
        {page.suggestions?.map((suggestion) => <Button key={suggestion.label} type="button" size="xs" variant="ghost" onClick={() => onPrefill(suggestion.prompt)}>{suggestion.label}</Button>)}
      </div>
    </div>
  )
}
