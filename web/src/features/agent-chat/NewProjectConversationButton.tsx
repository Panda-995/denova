import { useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useQuery } from '@tanstack/react-query'
import { Bot, Loader2, Plus } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import { customAgentsForRuntime } from '@/features/agents/CustomAgentSelect'
import { projectSettingsTarget, settingsQueryOptions } from '@/features/settings/query'
import { queryClient } from '@/lib/query-client'
import { toast } from '@/lib/toast'
import type { AgentChatProject } from './api'

/** Project-specific Agent choices are read on intent, not once for every row
 * during navigation. Every click revalidates settings before choosing whether
 * to create the default conversation or offer custom Agents. */
export function NewProjectConversationButton({ project, onCreate }: {
  project: AgentChatProject
  onCreate: (customAgentId?: string) => void
}) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [loading, setLoading] = useState(false)
  const pending = useRef(false)
  const options = settingsQueryOptions(projectSettingsTarget(project.id))
  const settings = useQuery({ ...options, enabled: false }, queryClient)
  const kind = project.type === 'book' ? 'ide' : 'general'
  const agents = customAgentsForRuntime(settings.data?.effective.custom_agents, kind)
  const activate = async (next: boolean) => {
    if (!next) { setOpen(false); return }
    if (pending.current) return
    pending.current = true
    setLoading(true)
    try {
      const fresh = await queryClient.fetchQuery({ ...options, staleTime: 0 })
      if (customAgentsForRuntime(fresh.effective.custom_agents, kind).length) setOpen(true)
      else onCreate()
    } catch (error) {
      console.error('[agent-chat] loading Project Agent choices failed', { projectId: project.id, error })
      toast.error(t('agentChat.sidebar.createFailed'))
    } finally {
      pending.current = false
      setLoading(false)
    }
  }
  return <DropdownMenu open={open} onOpenChange={next => void activate(next)}>
    <DropdownMenuTrigger asChild>
      <Button type="button" variant="ghost" size="icon-xs"
        className="shrink-0 opacity-60 transition-opacity hover:opacity-100 data-[state=open]:opacity-100 focus-visible:opacity-100"
        disabled={project.status !== 'available' || loading}
        aria-label={t('agentChat.sidebar.newChatIn', { name: project.name || project.path })}>
        {loading ? <Loader2 className="animate-spin" /> : <Plus />}
      </Button>
    </DropdownMenuTrigger>
    <DropdownMenuContent align="end" className="min-w-48">
      <DropdownMenuItem onSelect={() => onCreate('')}>{t(kind === 'ide' ? 'agents.ide.title' : 'agents.general.title')}</DropdownMenuItem>
      {agents.map(agent => <DropdownMenuItem key={agent.id} onSelect={() => onCreate(agent.id)}><Bot />{agent.name}</DropdownMenuItem>)}
    </DropdownMenuContent>
  </DropdownMenu>
}
