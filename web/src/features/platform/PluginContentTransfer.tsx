import { useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { fetchAPI, requestJSON } from '@/lib/api-client/client'
import { InlineErrorNotice } from '@/components/common/inline-error-notice'
import { platformError } from './api'

/** Transfers author content only. Imports never overwrite existing content. */
export function PluginContentTransfer({ projectId, pluginId }: { projectId: string; pluginId: string }) {
  const { t } = useTranslation()
  const input = useRef<HTMLInputElement>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [imported, setImported] = useState(false)
  const endpoint = `/api/platform/manage/projects/${projectId}/plugins/${pluginId}/content`
  const perform = async (work: () => Promise<void>) => {
    setError(''); setBusy(true)
    try { await work() } catch (error) { console.error('[plugins] content transfer failed', { projectId, pluginId, error }); setError(platformError(error)) }
    finally { setBusy(false) }
  }
  return <details className="text-sm"><summary className="cursor-pointer">{t('platform.plugins.content')}</summary>
    <p className="mt-2 text-muted-foreground">{t('platform.plugins.contentHelp')}</p>
    <div className="mt-2 flex flex-wrap gap-2">
      <Button variant="outline" size="sm" disabled={busy} onClick={() => void perform(async () => {
        const response = await fetchAPI(endpoint)
        if (!response.ok) { const body = await response.json(); throw new Error(t(body.messageKey)) }
        const url = URL.createObjectURL(await response.blob()), link = document.createElement('a')
        link.href = url; link.download = `${pluginId}-content.zip`; link.click(); setTimeout(() => URL.revokeObjectURL(url), 1000)
      })}>{t('platform.plugins.exportContent')}</Button>
      <Button variant="outline" size="sm" disabled={busy} onClick={() => input.current?.click()}>{t('platform.plugins.importContent')}</Button>
      <input ref={input} type="file" accept=".zip,application/zip" className="hidden" aria-label={t('platform.plugins.importContent')} onChange={event => {
        const file = event.target.files?.[0]; event.target.value = ''
        if (file) void perform(async () => { await requestJSON(endpoint, { method: 'POST', headers: { 'Content-Type': 'application/zip' }, body: file }); setImported(true) })
      }} />
    </div>
    {imported && <p role="status">{t('platform.plugins.contentImported')}</p>}
    {error && <InlineErrorNotice message={error} />}
  </details>
}
