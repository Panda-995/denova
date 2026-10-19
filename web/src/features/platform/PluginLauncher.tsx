import { useEffect, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Switch } from '@/components/ui/switch'
import { Field, FieldLabel } from '@/components/ui/field'
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription } from '@/components/ui/dialog'
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { InlineErrorNotice } from '@/components/common/inline-error-notice'
import { getAgentChatProjects } from '@/features/agent-chat/api'
import { PluginContentTransfer } from './PluginContentTransfer'
import { RuntimeSetup, type Setup } from './RuntimeSetup'
import { management, localized, platformError, type ContributionContext, type ProjectPlugin, type ProjectExtensionConfiguration } from './api'
import type { PluginActionSelection, PluginLauncherTarget } from './PluginWorkspace'

export function PluginLauncher({ target, onClose, onSelect }: {
  target: PluginLauncherTarget
  onClose: () => void
  onSelect: (selection: PluginActionSelection) => Promise<void>
}) {
  const { t, i18n } = useTranslation()
  const client = useQueryClient()
  const [projectId, setProjectId] = useState(target.projectId ?? '')
  const [scene, setScene] = useState<ContributionContext>(target.context ?? 'general')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const projects = useQuery({ queryKey: ['platform', 'projects'], queryFn: () => getAgentChatProjects() })
  const configuration = useQuery({ queryKey: ['platform', 'project-config', projectId], queryFn: () => management<ProjectExtensionConfiguration>(`/projects/${projectId}/extensions`), enabled: !!projectId })
  const plugins = useQuery({ queryKey: ['platform', 'project-plugins', projectId, scene, i18n.language], queryFn: () => management<ProjectPlugin[]>(`/projects/${projectId}/plugins?context=${scene}&locale=${i18n.language.startsWith('zh') ? 'zh-CN' : 'en-US'}`), enabled: !!projectId })
  const [models, setModels] = useState<Record<string, string>>({})
  useEffect(() => { setModels(configuration.data?.extensions.models ?? {}) }, [configuration.data])
  const projectName = projects.data?.find(project => project.id === projectId)?.name ?? projectId
  const compare = (a: { id: string; title: ProjectPlugin['name'] }, b: { id: string; title: ProjectPlugin['name'] }) => localized(a.title, i18n.language).localeCompare(localized(b.title, i18n.language), i18n.language) || a.id.localeCompare(b.id)
  const entries = (plugins.data ?? []).filter(plugin => !target.pluginId || target.pluginId === plugin.id).sort((a, b) => compare({ id: a.id, title: a.name }, { id: b.id, title: b.name }))
  const save = async (disabledPlugins = configuration.data?.extensions.disabledPlugins ?? []) => {
    if (!configuration.data) return
    const saved = await management<ProjectExtensionConfiguration>(`/projects/${projectId}/extensions`, 'PUT', { expectedRevision: configuration.data.revision, extensions: { disabledPlugins, models } })
    client.setQueryData(['platform', 'project-config', projectId], saved)
    await client.invalidateQueries({ queryKey: ['platform', 'project-plugins', projectId] })
  }
  const perform = async (work: () => Promise<void>) => {
    setError(''); setBusy(true)
    try { await work() } catch (error) { console.error('[plugins] project action failed', { projectId, error }); setError(platformError(error)) }
    finally { setBusy(false) }
  }
  const modelChanged = JSON.stringify(models) !== JSON.stringify(configuration.data?.extensions.models ?? {})
  const changeSetup = (setup: Setup) => { setProjectId(setup.projectId); setModels(setup.models) }
  return <Dialog open onOpenChange={open => { if (!open && !busy) onClose() }}>
    <DialogContent className="max-h-[90dvh] overflow-y-auto sm:max-w-2xl">
      <DialogHeader><DialogTitle>{t('platform.plugins.actions')}</DialogTitle><DialogDescription>{t('platform.plugins.description')}</DialogDescription></DialogHeader>
      <fieldset disabled={busy} className="flex min-w-0 flex-col gap-4">
        <RuntimeSetup projectLocked={!!target.projectId} value={{ projectId, models, configuration: {} }} onChange={changeSetup} />
        {target.projectId && <p className="break-words text-sm text-muted-foreground">{t('platform.project')}: {projectName}</p>}
        {!target.context && <Field><FieldLabel>{t('platform.plugins.context')}</FieldLabel><Select value={scene} onValueChange={value => setScene(value as ContributionContext)}><SelectTrigger className="w-full"><SelectValue /></SelectTrigger><SelectContent><SelectGroup>{(['writing', 'game', 'general'] as const).map(value => <SelectItem key={value} value={value}>{t('platform.plugins.context.' + value)}</SelectItem>)}</SelectGroup></SelectContent></Select></Field>}
        {(error || plugins.error || configuration.error) && <InlineErrorNotice message={error || platformError(plugins.error || configuration.error)} />}
        {projectId && plugins.isPending && <p role="status">{t('common.loading')}</p>}
        {projectId && plugins.isSuccess && entries.length === 0 && <p className="text-sm text-muted-foreground">{t('platform.plugins.empty')}</p>}
        {entries.map(plugin => <section key={plugin.id} className="flex min-w-0 flex-col gap-3 rounded-md border p-3">
          <div className="flex items-start justify-between gap-3"><h3 className="min-w-0 break-words font-medium">{localized(plugin.name, i18n.language)}</h3>
            <Switch aria-label={t('platform.plugins.projectEnabled', { name: localized(plugin.name, i18n.language) })} checked={!configuration.data?.extensions.disabledPlugins.includes(plugin.id)} disabled={!configuration.data} onCheckedChange={enabled => void perform(() => save(enabled ? configuration.data!.extensions.disabledPlugins.filter(id => id !== plugin.id) : [...configuration.data!.extensions.disabledPlugins, plugin.id]))} />
          </div>
          {!plugin.enabled && <p className="text-sm text-muted-foreground">{t('platform.plugins.disabled')}</p>}
          {plugin.problem && <InlineErrorNotice message={t(plugin.problem.messageKey)} />}
          {plugin.models.length > 0 && <RuntimeSetup projectLocked value={{ projectId, models, configuration: {} }} modelRequirements={plugin.models} onChange={changeSetup} />}
          <div className="flex flex-wrap gap-2">{plugin.actions.toSorted(compare).map(action => <Button key={action.kind + action.id} variant="outline" disabled={!plugin.enabled || !!plugin.problem || !configuration.data || plugin.models.some(slot => slot.required && !models[slot.key])} onClick={() => void perform(async () => {
            if (modelChanged) await save()
            await onSelect({ projectId, projectName, context: scene, plugin, action })
          })}>{localized(action.title, i18n.language)}</Button>)}</div>
          <PluginContentTransfer projectId={projectId} pluginId={plugin.id} />
          {plugin.actions.length === 0 && <p className="text-sm text-muted-foreground">{t('platform.plugins.toolsOnly')}</p>}
        </section>)}
        {modelChanged && <Button disabled={!configuration.data} onClick={() => void perform(() => save())}>{t('platform.plugins.saveModels')}</Button>}
      </fieldset>
    </DialogContent>
  </Dialog>
}
