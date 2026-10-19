import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useTheme } from 'next-themes'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter } from '@/components/ui/dialog'
import { InlineErrorNotice } from '@/components/common/inline-error-notice'
import { ConfigurationForm, configurationValidator } from './ConfigurationForm'
import { consumer, localized, management, releasePluginConsumer, platformError, type RuntimeSnapshot } from './api'
import { type PluginActionSelection } from './PluginWorkspace'

/** A command is a user invocation of the existing tool protocol. Cancellation
 * releases its consumer; uncertain third-party effects are never retried here. */
export function PluginCommandDialog({ selection, onClose }: { selection: PluginActionSelection; onClose: () => void }) {
  const { t, i18n } = useTranslation()
  const { resolvedTheme } = useTheme()
  const [input, setInput] = useState<Record<string, unknown>>({})
  const [busy, setBusy] = useState(false)
  const [result, setResult] = useState('')
  const [error, setError] = useState('')
  const abort = useRef<AbortController | null>(null)
  const activeRuntime = useRef<RuntimeSnapshot | null>(null)
  const mounted = useRef(true)
  useEffect(() => {
    mounted.current = true
    const release = () => {
      abort.current?.abort()
      if (activeRuntime.current) void releasePluginConsumer(activeRuntime.current).catch(error => console.error('[plugins] release unloaded command failed', error))
    }
    window.addEventListener('pagehide', release)
    return () => { mounted.current = false; window.removeEventListener('pagehide', release); release() }
  }, [])
  const run = async () => {
    if (busy || !selection.action.form) return
    if (configurationValidator.validateFormData(input, selection.action.form.schema).errors.length) { setError(t('platform.settings.invalidValue')); return }
    const controller = new AbortController()
    abort.current = controller
    setBusy(true); setResult(''); setError('')
    let runtime: RuntimeSnapshot | undefined
    try {
      // Await admission even after cancellation so a late runtime is released.
      runtime = await management<RuntimeSnapshot>('/plugin-actions/open', 'POST', {
        projectId: selection.projectId, pluginId: selection.plugin.id, releaseId: selection.plugin.releaseId, context: selection.context,
        kind: 'command', actionId: selection.action.id, consumerId: crypto.randomUUID(), locale: i18n.language, theme: resolvedTheme,
      })
      activeRuntime.current = runtime
      controller.signal.throwIfAborted()
      const response = await consumer<{ content: string; data?: unknown }>(runtime, `/tools/${selection.action.target.id}/invoke`, 'POST', { input }, controller.signal)
      if (mounted.current) setResult([response.content, response.data === undefined ? '' : JSON.stringify(response.data, null, 2)].filter(Boolean).join('\n\n'))
    } catch (error) {
      if (!controller.signal.aborted) console.error('[plugins] command failed', { plugin: selection.plugin.id, command: selection.action.id, error })
      if (mounted.current) setError(controller.signal.aborted ? t('platform.plugins.cancelled') : platformError(error))
    } finally {
      if (runtime) await releasePluginConsumer(runtime).catch(error => { console.error('[plugins] release command failed', error); if (mounted.current) setError(platformError(error)) })
      activeRuntime.current = null
      if (mounted.current) setBusy(false)
      abort.current = null
    }
  }
  return <Dialog open onOpenChange={open => { if (!open && !busy) onClose() }}>
    <DialogContent className="max-h-[90dvh] overflow-y-auto sm:max-w-2xl">
      <DialogHeader><DialogTitle>{localized(selection.action.title, i18n.language)}</DialogTitle><DialogDescription>{localized(selection.plugin.name, i18n.language)} · {selection.projectName}</DialogDescription></DialogHeader>
      {selection.action.form && <ConfigurationForm definition={selection.action.form} values={input} disabled={busy} onChange={setInput} />}
      {error && <InlineErrorNotice message={error} />}
      {busy && <p role="status">{t('platform.plugins.running')}</p>}
      {result && <div className="flex min-w-0 flex-col gap-2"><pre className="max-h-[45dvh] overflow-auto whitespace-pre-wrap break-words rounded-md bg-muted p-3 text-sm">{result}</pre><Button variant="outline" onClick={() => void navigator.clipboard.writeText(result).catch(error => setError(platformError(error)))}>{t('platform.plugins.copyResult')}</Button></div>}
      <DialogFooter>{busy ? <Button variant="outline" onClick={() => abort.current?.abort()}>{t('platform.plugins.cancel')}</Button> : <Button onClick={() => void run()}>{t('platform.plugins.run')}</Button>}</DialogFooter>
    </DialogContent>
  </Dialog>
}
