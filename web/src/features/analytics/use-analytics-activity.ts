import { useEffect } from 'react'
import { useWorkspaceStore } from '@/stores/workspace-store'
import { useInteractiveStore } from '@/features/interactive/stores/interactive-store'
import { ANALYTICS_PREFERENCES_CHANGED, fetchAnalyticsPreferences, recordActivity, type AnalyticsFeature, type AnalyticsPreferences } from './api'

const routeFeatures: Readonly<Record<string, AnalyticsFeature>> = {
  ide: 'writing', interactive: 'game', agentchat: 'chat', lore: 'library',
  versions: 'versions', automations: 'automation', books: 'projects', market: 'resources',
}

/** Focused reading is a category-only milestone, not an input-event recorder. */
export function useAnalyticsActivity(blocked: boolean, hasWritingContent: boolean) {
  const mode = useWorkspaceStore(state => state.mode)
  const hasGameContent = useInteractiveStore(state => (state.snapshot?.turns.length ?? 0) > 0)
  useEffect(() => {
    const feature = routeFeatures[mode]
    if (!feature || blocked || (mode === 'ide' && !hasWritingContent) || (mode === 'interactive' && !hasGameContent)) return
    let active = true
    let enabled = false
    let timer: ReturnType<typeof setTimeout> | undefined
    let remaining = 30_000
    let started = 0
    let recorded = false
    const pause = () => {
      if (timer !== undefined) {
        clearTimeout(timer); timer = undefined
        remaining = Math.max(0, remaining - (performance.now() - started))
      }
    }
    const resume = () => {
      pause()
      if (!active || !enabled || recorded || document.visibilityState !== 'visible' || !document.hasFocus()) return
      started = performance.now()
      timer = setTimeout(() => {
        timer = undefined; recorded = true
        void recordActivity(feature).catch(cause => console.warn('[analytics] Failed to record a reading milestone', cause))
      }, remaining)
    }
    const apply = (preferences: AnalyticsPreferences) => {
      enabled = preferences.available && preferences.enabled && preferences.notice_seen
      if (!enabled) {
        pause()
        remaining = 30_000
        recorded = false
      }
      resume()
    }
    void fetchAnalyticsPreferences().then(value => { if (active) apply(value) })
      .catch(cause => console.warn('[analytics] Failed to load activity preferences', cause))
    const onChange = (event: Event) => apply((event as CustomEvent<AnalyticsPreferences>).detail)
    window.addEventListener(ANALYTICS_PREFERENCES_CHANGED, onChange)
    document.addEventListener('visibilitychange', resume)
    window.addEventListener('focus', resume)
    window.addEventListener('blur', pause)
    return () => {
      active = false; pause()
      window.removeEventListener(ANALYTICS_PREFERENCES_CHANGED, onChange)
      document.removeEventListener('visibilitychange', resume)
      window.removeEventListener('focus', resume)
      window.removeEventListener('blur', pause)
    }
  }, [mode, blocked, hasWritingContent, hasGameContent])
}
