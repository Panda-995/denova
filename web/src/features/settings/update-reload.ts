import { fetchAPI } from '@/lib/api-client'
import type { UpdateStatus } from './types'

export const UPDATE_RELOAD_TIMEOUT_MS = 180_000
const POLL_INTERVAL_MS = 1000

type ReloadOptions = {
  pollBackend?: (signal: AbortSignal) => Promise<UpdateStatus>
  reload?: (href: string) => void
  onError: (reason: 'failed' | 'timeout', status?: UpdateStatus) => void
  currentHref?: string
}

// The installation ID prevents an old server or a different update from being
// mistaken for success. Cancellation belongs to the settings hook's lifetime.
export function scheduleFrontendReloadAfterUpdate(target: { id: string; version: string }, options: ReloadOptions) {
  const controller = new AbortController()
  const deadline = Date.now() + UPDATE_RELOAD_TIMEOUT_MS
  let timer: ReturnType<typeof setTimeout>
  const poll = async () => {
    if (controller.signal.aborted) return
    if (Date.now() >= deadline) { options.onError('timeout'); return }
    try {
      const status = await (options.pollBackend ?? defaultPollBackend)(AbortSignal.any([controller.signal, AbortSignal.timeout(5000)]))
      if (controller.signal.aborted) return
      if (status.id === target.id) {
        if (status.phase === 'failed') { options.onError('failed', status); return }
        if (status.phase === 'succeeded' && status.current_version.replace(/^v/, '') === target.version.replace(/^v/, '')) {
          const url = buildFrontendReloadURL(target.version, Date.now(), options.currentHref ?? window.location.href)
          ;(options.reload ?? ((href) => window.location.replace(href)))(url)
          return
        }
      }
    } catch { /* The backend is expected to disconnect while restarting. */ }
    if (!controller.signal.aborted) timer = setTimeout(() => void poll(), POLL_INTERVAL_MS)
  }
  timer = setTimeout(() => void poll(), POLL_INTERVAL_MS)
  return () => { controller.abort(); clearTimeout(timer) }
}

export function buildFrontendReloadURL(version: string, nonce: number, currentHref: string) {
  const url = new URL(currentHref)
  url.searchParams.set('denova_reload', `${version}-${nonce}`)
  return url.toString()
}

async function defaultPollBackend(signal: AbortSignal): Promise<UpdateStatus> {
  const res = await fetchAPI('/api/update/status', { signal, cache: 'no-store', suppressBackendUnavailableToast: true })
  if (!res.ok) throw new Error(`Update status HTTP ${res.status}`)
  return res.json()
}
