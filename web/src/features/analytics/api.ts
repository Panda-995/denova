import { requestJSON } from '@/lib/api-client'

export interface AnalyticsPreferences {
  enabled: boolean
  notice_seen: boolean
  available: boolean
}
export type AnalyticsFeature = 'writing' | 'game' | 'chat' | 'image' | 'library' | 'versions' | 'automation' | 'projects' | 'resources'
export const ANALYTICS_PREFERENCES_CHANGED = 'denova:analytics-preferences-changed'
export function fetchAnalyticsPreferences() { return requestJSON<AnalyticsPreferences>('/api/analytics') }
export async function setAnalyticsEnabled(enabled: boolean) {
  const preferences = await requestJSON<AnalyticsPreferences>('/api/analytics', {
    method: 'PATCH', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ enabled }),
  })
  window.dispatchEvent(new CustomEvent(ANALYTICS_PREFERENCES_CHANGED, { detail: preferences }))
  return preferences
}
// Only a category is sent. Browser URLs, titles, editor content and input
// event details never enter this request.
export function recordActivity(feature: AnalyticsFeature) {
  return requestJSON('/api/analytics/activity', {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ feature }),
  })
}
