import { useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { GLOBAL_SETTINGS_TARGET, projectSettingsTarget, settingsQueryOptions } from '@/features/settings/query'
import type { AgentRuntimeKind, CustomAgentConfig } from '@/features/settings/types'
import { cn } from '@/lib/utils'
import { runtimeKindForContract } from './agent-contracts'

const BUILTIN_VALUE = '__builtin_agent__'
const INHERIT_VALUE = '__inherit_agent__'

interface CustomAgentSelectProps {
  projectId?: string
  runtimeKind: AgentRuntimeKind
  value?: string
  onValueChange: (customAgentId: string | undefined) => void
  inheritLabel?: string
  disabled?: boolean
  size?: 'sm' | 'default'
  className?: string
}

/** Selects one Agent instance without exposing runtime kinds as user-defined code. */
export function CustomAgentSelect({ projectId = '', runtimeKind, value, onValueChange, inheritLabel, disabled = false, size, className }: CustomAgentSelectProps) {
  const { t } = useTranslation()
  const triggerRef = useRef<HTMLButtonElement>(null)
  const [triggerWidth, setTriggerWidth] = useState<number>()
  const compact = size === 'sm'
  // Match the trigger's border and inline spacing to preserve alignment at fractional display scales.
  const itemClassName = compact
    ? 'h-7 border border-transparent pr-[30px] pl-2.5 [&>span:last-child]:block [&>span:last-child]:min-w-0 [&>span:last-child]:truncate'
    : undefined
  // Portal options keep the compact typography while retaining mobile minimum touch sizes.
  const itemStyle = compact ? {
    fontSize: 'var(--nova-ui-compact-font-size)',
    lineHeight: 'var(--nova-ui-compact-line-height)',
    paddingBlock: 0,
    borderRadius: 'min(var(--radius-md),10px)',
  } : undefined
  const target = projectId.trim() ? projectSettingsTarget(projectId) : GLOBAL_SETTINGS_TARGET
  const query = useQuery(settingsQueryOptions(target))
  const catalog = query.data?.effective.custom_agents
  const agents = customAgentsForRuntime(catalog, runtimeKind)
  const archivedSelection = value
    ? catalog?.find((agent) => agent.id === value && runtimeKindForContract(agent.contract) === runtimeKind && agent.enabled === false)
    : undefined
  const baseTitle = t(runtimeAgentTitleKey(runtimeKind))
  let selectedValue = value || BUILTIN_VALUE
  if (value === undefined && inheritLabel) selectedValue = INHERIT_VALUE

  const handleValueChange = (next: string) => {
    if (next === INHERIT_VALUE) {
      onValueChange(undefined)
      return
    }
    onValueChange(next === BUILTIN_VALUE ? '' : next)
  }

  return (
    <Select
      value={selectedValue}
      onValueChange={handleValueChange}
      disabled={disabled || query.isLoading}
      onOpenChange={(open) => {
        if (open && compact) setTriggerWidth(triggerRef.current?.getBoundingClientRect().width)
      }}
    >
      <SelectTrigger ref={triggerRef} size={size} className={cn(compact && 'nova-ui-compact', className)} aria-label={t('agents.custom.select')}>
        <SelectValue />
      </SelectTrigger>
      <SelectContent
        className={cn(compact && 'min-w-0 rounded-[min(var(--radius-md),10px)]')}
        style={compact ? { width: triggerWidth, borderRadius: itemStyle?.borderRadius } : undefined}
      >
        <SelectGroup className="p-0">
          {inheritLabel ? <SelectItem value={INHERIT_VALUE} className={itemClassName} style={itemStyle}>{inheritLabel}</SelectItem> : null}
          <SelectItem value={BUILTIN_VALUE} className={itemClassName} style={itemStyle}>{baseTitle}</SelectItem>
          {archivedSelection?.id ? (
            <SelectItem value={archivedSelection.id} className={itemClassName} style={itemStyle} disabled>
              {archivedSelection.name || archivedSelection.id} · {t('agents.custom.archived')}
            </SelectItem>
          ) : null}
          {agents.map((agent) => <SelectItem key={agent.id} value={agent.id!} className={itemClassName} style={itemStyle}>{agent.name}</SelectItem>)}
        </SelectGroup>
      </SelectContent>
    </Select>
  )
}

export function customAgentsForRuntime(agents: CustomAgentConfig[] | undefined, runtimeKind: AgentRuntimeKind) {
  return (agents ?? []).filter((agent) => (
    agent.enabled !== false
    && runtimeKindForContract(agent.contract) === runtimeKind
    && Boolean(agent.id?.trim())
    && Boolean(agent.name?.trim())
  ))
}

function runtimeAgentTitleKey(runtimeKind: AgentRuntimeKind) {
  switch (runtimeKind) {
    case 'general': return 'agents.general.title'
    case 'ide': return 'agents.ide.title'
    case 'interactive_story': return 'agents.interactiveStory.title'
    case 'image': return 'agents.image.title'
  }
}
