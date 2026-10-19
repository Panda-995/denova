import { createContext, lazy, Suspense, useContext, useState, useCallback, useRef, type ReactNode } from 'react'
import { Maximize2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import type { AskChatMessage, ToolCallChatMessage, ToolResultChatMessage } from '@/lib/api'
import { Dialog } from '@/components/ui/dialog'
import { TooltipIconButton } from '@/components/common/tooltip-icon-button'
import { cn } from '@/lib/utils'

export type InspectableToolMessage = ToolCallChatMessage | ToolResultChatMessage | AskChatMessage

type OpenInspector = (message: InspectableToolMessage, trigger: HTMLElement) => void
const InspectorHostContext = createContext<OpenInspector | null>(null)
const InspectorContext = createContext<((trigger: HTMLElement) => void) | null>(null)
const ToolInspectorDialog = lazy(() => import('./ToolInspectorDialog'))

/** The dialog belongs to the transcript, so virtual row unmounts cannot close it. */
export function ToolInspectorHost({ children, projectId = '', resolveMessage, restoreFocus }: {
  children: ReactNode
  projectId?: string
  resolveMessage?: (message: InspectableToolMessage) => InspectableToolMessage | undefined
  restoreFocus?: () => HTMLElement | null
}) {
  const [selected, setSelected] = useState<InspectableToolMessage | null>(null)
  const triggerRef = useRef<HTMLElement | null>(null)
  const open = useCallback<OpenInspector>((message, trigger) => {
    triggerRef.current = trigger
    setSelected(message)
  }, [])
  const message = selected && (resolveMessage?.(selected) || selected)
  return (
    <InspectorHostContext.Provider value={open}>
      <Dialog open={!!selected} onOpenChange={isOpen => { if (!isOpen) setSelected(null) }}>
        {children}
        {message && <Suspense fallback={null}>
          <ToolInspectorDialog message={message} projectId={projectId} onCloseAutoFocus={event => {
            event.preventDefault()
            const target = triggerRef.current?.isConnected ? triggerRef.current : restoreFocus?.()
            target?.focus({ preventScroll: true })
          }} />
        </Suspense>}
      </Dialog>
    </InspectorHostContext.Provider>
  )
}

/** Standalone cards use the same host with their current message. */
export function ToolInspector({ message, projectId, children }: { message: InspectableToolMessage; projectId: string; children: ReactNode }) {
  const open = useContext(InspectorHostContext)
  if (!open) return <ToolInspectorHost projectId={projectId} resolveMessage={() => message}><ToolInspector message={message} projectId={projectId}>{children}</ToolInspector></ToolInspectorHost>
  return <InspectorContext.Provider value={trigger => open(message, trigger)}>{children}</InspectorContext.Provider>
}

/** Collapsible cards expose this secondary action only while their content is open. */
export function ToolInspectorButton({ className }: { className?: string }) {
  const available = useContext(InspectorContext)
  const { t } = useTranslation()
  if (!available) return null
  return (
      <TooltipIconButton label={t('chat.tool.inspector.open')} tooltipSide="top" className={cn('shrink-0 text-[var(--nova-text-faint)] hover:text-[var(--nova-text)]', className)} onClick={(event) => { event.stopPropagation(); available(event.currentTarget) }}>
        <Maximize2 />
      </TooltipIconButton>
  )
}
