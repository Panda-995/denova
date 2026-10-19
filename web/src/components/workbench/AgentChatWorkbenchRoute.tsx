import { memo } from 'react'
import loadable from '@loadable/component'
import type { ComponentProps } from 'react'
import type { AgentChatRoute as AgentChatRouteComponent } from '@/features/agent-chat/AgentChatRoute'
import { RetainedWorkbenchRouteLayer, workbenchLoadingOptions } from './WorkbenchRouteHost'

const AgentChatRoute = memo(loadable(() => import('@/features/agent-chat/AgentChatRoute').then((module) => ({ default: module.AgentChatRoute })), workbenchLoadingOptions))

interface AgentChatWorkbenchRouteProps extends ComponentProps<typeof AgentChatRouteComponent> {
  mounted: boolean
  visible: boolean
  loadingLabel: string
  retentionKey: string
}

/** Keeps the shared Agent Chat project mounted without coupling it to Writing layout state. */
export function AgentChatWorkbenchRoute({
  mounted,
  visible,
  loadingLabel,
  retentionKey,
  ...routeProps
}: AgentChatWorkbenchRouteProps) {
  if (!mounted) return null
  return (
    <RetainedWorkbenchRouteLayer visible={visible} loadingLabel={loadingLabel} retentionKey={retentionKey}>
      <AgentChatRoute {...routeProps} />
    </RetainedWorkbenchRouteLayer>
  )
}
