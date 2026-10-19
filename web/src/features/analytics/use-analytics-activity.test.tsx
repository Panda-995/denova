import { act, cleanup, renderHook } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useWorkspaceStore } from '@/stores/workspace-store'
import { useInteractiveStore } from '@/features/interactive/stores/interactive-store'
import { useAnalyticsActivity } from './use-analytics-activity'
import { ANALYTICS_PREFERENCES_CHANGED, fetchAnalyticsPreferences, recordActivity } from './api'

vi.mock('./api', () => ({
  ANALYTICS_PREFERENCES_CHANGED: 'denova:analytics-preferences-changed',
  fetchAnalyticsPreferences: vi.fn(), recordActivity: vi.fn(),
}))

describe('content-free foreground activity', () => {
  beforeEach(() => {
    vi.useFakeTimers(); vi.clearAllMocks()
    vi.spyOn(document, 'hasFocus').mockReturnValue(true)
    vi.spyOn(performance, 'now').mockImplementation(() => Date.now())
    vi.mocked(fetchAnalyticsPreferences).mockResolvedValue({ enabled: true, notice_seen: true, available: true })
    vi.mocked(recordActivity).mockResolvedValue({})
    useWorkspaceStore.setState({ mode: 'ide' })
    useInteractiveStore.setState({ snapshot: null })
  })
  afterEach(() => { cleanup(); vi.useRealTimers(); vi.restoreAllMocks() })

  it('does not count empty Writing/Game destinations as reading', async () => {
    const writing = renderHook(() => useAnalyticsActivity(false, false))
    await act(async () => { await vi.advanceTimersByTimeAsync(31_000) })
    writing.unmount()
    useWorkspaceStore.setState({ mode: 'interactive' })
    renderHook(() => useAnalyticsActivity(false, true))
    await act(async () => { await vi.advanceTimersByTimeAsync(31_000) })
    expect(recordActivity).not.toHaveBeenCalled()
  })

  it('pauses background reading and sends only the fixed feature category', async () => {
    renderHook(() => useAnalyticsActivity(false, true))
    await act(async () => {})
    await act(async () => { await vi.advanceTimersByTimeAsync(29_000) })
    act(() => window.dispatchEvent(new Event('blur')))
    await act(async () => { await vi.advanceTimersByTimeAsync(60_000) })
    expect(recordActivity).not.toHaveBeenCalled()
    act(() => window.dispatchEvent(new Event('focus')))
    await act(async () => { await vi.advanceTimersByTimeAsync(1000) })
    expect(recordActivity).toHaveBeenCalledExactlyOnceWith('writing')
  })

  it('does not carry reading time across disabling and re-enabling statistics', async () => {
    renderHook(() => useAnalyticsActivity(false, true))
    await act(async () => {})
    await act(async () => { await vi.advanceTimersByTimeAsync(29_000) })
    act(() => window.dispatchEvent(new CustomEvent(ANALYTICS_PREFERENCES_CHANGED, { detail: { enabled: false, notice_seen: true, available: true } })))
    await act(async () => { await vi.advanceTimersByTimeAsync(60_000) })
    act(() => window.dispatchEvent(new CustomEvent(ANALYTICS_PREFERENCES_CHANGED, { detail: { enabled: true, notice_seen: true, available: true } })))
    await act(async () => { await vi.advanceTimersByTimeAsync(29_000) })
    expect(recordActivity).not.toHaveBeenCalled()
    await act(async () => { await vi.advanceTimersByTimeAsync(1000) })
    expect(recordActivity).toHaveBeenCalledExactlyOnceWith('writing')
  })
})
