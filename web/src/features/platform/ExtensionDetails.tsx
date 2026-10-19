import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { ChevronDown, Download, FolderOpen, Gamepad2, MoreHorizontal, Play, Puzzle, Square, Trash2 } from 'lucide-react'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from '@/components/ui/card'
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter } from '@/components/ui/dialog'
import { DropdownMenu, DropdownMenuContent, DropdownMenuGroup, DropdownMenuItem, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import { InlineErrorNotice } from '@/components/common/inline-error-notice'
import { cn } from '@/lib/utils'
import { useWorkspaceStore } from '@/stores/workspace-store'
import { management, localized, platformError, type RuntimeSnapshot } from './api'
import type { Installation } from '@/features/market/api'
import { type ExtensionEntry } from './extension-directory'
import { importExtensionSource, openExtensionSource, startExtensionGame } from './extension-navigation'
import { InstalledExtensionSettings } from './InstalledExtensionSettings'
import { InstalledExtensionToggle } from './InstalledExtensionToggle'
import { ExtensionSource } from './ExtensionSource'
import { PluginActionsButton } from './PluginWorkspace'

export function ExtensionDetails({ entry, runtimes, active, dirty, onDirtyChange, onRefresh, owner, onUpdate, blockedReason }: {
  entry: ExtensionEntry
  runtimes: RuntimeSnapshot[]
  active: boolean
  dirty: boolean
  /** Keep this editor mounted while it has unsaved changes or a pending save. */
  onDirtyChange: (dirty: boolean) => void
  onRefresh: () => Promise<void>
  owner?: Installation
  onUpdate: (installation: Installation) => Promise<void>
  blockedReason?: string
}) {
  const { t, i18n } = useTranslation()
  const { installed: item, manifest, sources } = entry
  const [confirm, setConfirm] = useState(false)
  const [busy, setBusy] = useState(false)
  const [failedCover, setFailedCover] = useState('')
  const current = item.releases.find(release => release.ref.releaseId === item.currentRelease)
  const activeRuntimes = runtimes.filter(runtime => runtime.context.environment === 'installed' && runtime.context.source.package.kind === item.kind && runtime.context.source.package.id === item.id)
  const name = localized(manifest?.name, i18n.language) || item.id
  const description = localized(manifest?.description, i18n.language)
  const Icon = item.kind === 'game' ? Gamepad2 : Puzzle
  const cover = item.kind === 'game' && manifest?.game?.cover && current
    ? `/api/platform/manage/packages/game/${encodeURIComponent(item.id)}/cover?releaseId=${encodeURIComponent(current.ref.releaseId)}` : ''
  const showCover = Boolean(cover && failedCover !== cover)
  const hasSidebar = Boolean(owner || activeRuntimes.length || manifest?.requires?.length || item.source)
  const run = async (action: () => Promise<void>) => {
    setBusy(true)
    try { await action() } catch (cause) {
      console.error('[extensions] installed package operation failed', { kind: item.kind, packageId: item.id, cause })
      toast.error(platformError(cause))
    } finally { setBusy(false) }
  }
  return <article data-testid="installed-extension-detail" className="@container/extension mx-auto flex w-full max-w-[1600px] flex-col gap-4 p-4 lg:p-5">
    <Card size="sm" className="gap-3">
    <CardHeader className="flex min-w-0 flex-row items-start gap-3">
      {showCover && <img src={cover} alt={t('platform.extensions.cover', { name })} className="aspect-[3/4] w-[clamp(4rem,12cqw,6rem)] shrink-0 rounded-lg border object-cover" onError={() => {
        console.error('[extensions] loading game cover failed', { packageId: item.id, releaseId: current?.ref.releaseId })
        setFailedCover(cover)
      }} />}
      <div className="flex min-w-0 flex-1 flex-col gap-2">
        <div className="flex flex-wrap items-start gap-3">
          <div className="flex min-w-[min(100%,12rem)] flex-1 items-center gap-2">
            {!showCover && <Icon className="size-5 shrink-0 text-muted-foreground" aria-hidden="true" />}
            <CardTitle><h1 className="text-lg font-semibold tracking-tight [overflow-wrap:anywhere]">{name}</h1></CardTitle>
          </div>
          <div className="ml-auto flex items-center gap-2">
            <InstalledExtensionToggle item={item} onChanged={onRefresh} disabled={busy} />
            <DropdownMenu>
              <DropdownMenuTrigger asChild><Button variant="ghost" size="icon-sm" disabled={busy} aria-label={t('platform.extensions.manageNamed', { name })}><MoreHorizontal /></Button></DropdownMenuTrigger>
              <DropdownMenuContent align="end"><DropdownMenuGroup><DropdownMenuItem variant="destructive" onSelect={() => setConfirm(true)}><Trash2 />{t('platform.uninstall')}</DropdownMenuItem></DropdownMenuGroup></DropdownMenuContent>
            </DropdownMenu>
          </div>
        </div>
        <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
          <Badge variant="secondary">{t('platform.type.' + item.kind)}</Badge>
          {manifest && <span>v{manifest.version}</span>}
          <span className="select-text [overflow-wrap:anywhere]">{item.id}</span>
        </div>
        <CardDescription className="whitespace-pre-wrap text-sm leading-relaxed [overflow-wrap:anywhere]">{description || t(item.kind === 'game' ? 'platform.extensions.gameHelp' : 'platform.extensions.pluginHelp')}</CardDescription>
      </div>
    </CardHeader>
    {(item.unavailableReason || (item.kind === 'plugin' && manifest?.contributes?.tools?.length)) && <CardContent className="flex flex-col gap-2">
      {item.unavailableReason && <InlineErrorNotice message={t(item.unavailableReason)} />}
      {item.kind === 'plugin' && Boolean(manifest?.contributes?.tools?.length) && <p className="text-xs leading-relaxed text-muted-foreground">{t('platform.extensions.pluginToolsHelp')}</p>}
    </CardContent>}
    <CardFooter className="flex-wrap gap-2 py-2">
        <div className="flex flex-wrap items-center gap-2">
          {item.kind === 'plugin' && <PluginActionsButton pluginId={item.id} label={t('platform.plugins.open')} />}
          {item.kind === 'game' && <Button size="sm" disabled={busy || !item.enabled || Boolean(item.unavailableReason) || !current} onClick={() => startExtensionGame(item.id)}><Play data-icon="inline-start" />{t('platform.extensions.startGame')}</Button>}
          {sources.length === 1 && <Button variant="outline" size="sm" disabled={busy} onClick={() => void run(() => openExtensionSource(sources[0]))}><FolderOpen data-icon="inline-start" />{t('platform.openSource')}</Button>}
          {sources.length > 1 && <DropdownMenu><DropdownMenuTrigger asChild><Button variant="outline" size="sm" disabled={busy}><FolderOpen data-icon="inline-start" />{t('platform.openSource')}<ChevronDown data-icon="inline-end" /></Button></DropdownMenuTrigger>
            <DropdownMenuContent align="start" className="max-w-[min(22rem,90vw)]"><DropdownMenuGroup>{sources.map(source => <DropdownMenuItem key={source.developmentId} className="break-all" onSelect={() => void run(() => openExtensionSource(source))}>{source.projectName} · {source.relativePath}</DropdownMenuItem>)}</DropdownMenuGroup></DropdownMenuContent>
          </DropdownMenu>}
          {sources.length === 0 && <Button variant="outline" size="sm" title={t('platform.extensions.noSourceHelp')} onClick={() => useWorkspaceStore.getState().setMode('agentchat')}><FolderOpen data-icon="inline-start" />{t('platform.openWorkbench')}</Button>}
          {current && <Button variant="outline" size="sm" asChild>
            <a href={`/api/platform/manage/packages/${item.kind}/${encodeURIComponent(item.id)}/archive?releaseId=${encodeURIComponent(current.ref.releaseId)}`}><Download data-icon="inline-start" />{t('platform.exportPackage')}</a>
          </Button>}
        </div>
        {item.kind === 'game' && <p className="text-xs text-muted-foreground">{t('platform.extensions.startGameHelp')}</p>}
    </CardFooter>
    </Card>
    <div className={cn('grid min-w-0 items-start gap-4', hasSidebar && '@4xl/extension:grid-cols-[minmax(0,2fr)_minmax(0,1fr)]')}>
    {current && <Card size="sm" className="min-w-0"><CardContent>
      <section aria-label={t('platform.usageSettings')}>
        <InstalledExtensionSettings key={current.ref.releaseId} item={item} release={current} active={active} onDirtyChange={onDirtyChange} onSaved={() => { void onRefresh(); toast.success(t('platform.settingsSaved')) }} />
      </section>
    </CardContent></Card>}
    {hasSidebar && <div className="flex min-w-0 flex-col gap-4">
    {owner && <Card size="sm"><CardHeader><CardTitle>{t('market.acquired.source')}</CardTitle></CardHeader><CardContent><ExtensionSource key={owner.installation_id} owner={owner} blockedReason={blockedReason} onUpdate={onUpdate} /></CardContent></Card>}
    {activeRuntimes.length > 0 && <Card size="sm"><CardHeader>
      <CardTitle><h2>{t('platform.runningCount', { count: activeRuntimes.length })}</h2></CardTitle><CardDescription className="text-xs">{t('platform.extensions.runningHelp')}</CardDescription>
      </CardHeader><CardFooter className="py-2">
      <Button variant="outline" size="sm" disabled={busy} onClick={() => void run(async () => {
        const results = await Promise.allSettled(activeRuntimes.map(runtime => management(`/runtimes/${runtime.id}/stop`, 'POST', {})))
        await onRefresh()
        const failure = results.find(result => result.status === 'rejected')
        if (failure?.status === 'rejected') throw failure.reason
      })}><Square data-icon="inline-start" />{t('platform.stopRunning')}</Button>
    </CardFooter></Card>}
    {Boolean(manifest?.requires?.length) && <Card size="sm"><CardHeader><CardTitle><h2>{t('platform.dependency')}</h2></CardTitle></CardHeader><CardContent>
      <ul className="divide-y">{manifest?.requires?.map(dependency => <li key={dependency.pluginId} className="flex flex-wrap items-center justify-between gap-2 py-2 text-xs"><span className="[overflow-wrap:anywhere]">{dependency.pluginId}</span><Badge variant="outline">{dependency.versionRange}</Badge></li>)}</ul>
    </CardContent></Card>}
    {item.source && <Card size="sm"><CardContent><details>
      <summary className="cursor-pointer text-sm text-muted-foreground">{t('market.development')}</summary>
      <div className="mt-3 flex flex-col gap-3">
        <p className="text-sm text-muted-foreground">{t('platform.github.buildHelp')}</p>
        <div><Button variant="outline" size="sm" disabled={busy || dirty} onClick={() => void run(() => importExtensionSource(item.source!))}>{t('platform.github.importSource')}</Button></div>
      </div>
    </details></CardContent></Card>}
    </div>}
    </div>
    <Dialog open={confirm} onOpenChange={value => { if (!busy) setConfirm(value) }}>
      <DialogContent>
        <DialogHeader><DialogTitle>{t('platform.uninstall')}</DialogTitle><DialogDescription>{t('platform.uninstallDescription')}</DialogDescription></DialogHeader>
        <DialogFooter>
          <Button variant="outline" disabled={busy} onClick={() => setConfirm(false)}>{t('common.cancel')}</Button>
          <Button variant="destructive" disabled={busy} onClick={() => void run(async () => {
            await management(`/packages/${item.kind}/${item.id}`, 'PATCH', { enabled: false, removed: true })
            setConfirm(false); onDirtyChange(false); await onRefresh()
          })}>{t('platform.uninstall')}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  </article>
}
