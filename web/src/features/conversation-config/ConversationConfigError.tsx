import { useTranslation } from 'react-i18next'
import { InlineErrorNotice } from '@/components/common/inline-error-notice'
import { Button } from '@/components/ui/button'
import { useToolNavigation } from '@/components/Chat/tool-navigation'
import type { ConversationConfigController } from './types'

/** Keeps recovery outside controls that require a successfully loaded snapshot. */
export function ConversationConfigError({ controller, agentKey }: {
  controller: ConversationConfigController
  agentKey?: string
}) {
  const { t } = useTranslation()
  const navigation = useToolNavigation()
  if (!controller.error) return null
  const id = controller.snapshot?.custom_agent_id || controller.snapshot?.agent_kind || agentKey
  return <div className="pointer-events-auto mb-2 min-w-0 space-y-1">
    <InlineErrorNotice message={controller.error} title={t('chat.modelProfile.configError')} />
    <div className="flex flex-wrap gap-1">
      <Button variant="outline" size="xs" disabled={controller.loading || controller.saving} onClick={() => void controller.reload()}>{t('common.retry')}</Button>
      {navigation && id && <Button variant="outline" size="xs" onClick={() => navigation.open({ kind: 'config_resource', resource: 'agent_profile', id, scope: 'user', section: 'runtime' })}>{t('agentRuntime.configure')}</Button>}
    </div>
  </div>
}
