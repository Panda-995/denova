import { act, renderHook, waitFor } from '@testing-library/react'
import { expect, it, vi } from 'vitest'
import { applyUpdate, getUpdateStatus, installUpdateStream } from './api'
import { useUpdateSettings } from './UpdateSettings'
import { scheduleFrontendReloadAfterUpdate } from './update-reload'

vi.mock('./api', () => ({
  getUpdateStatus: vi.fn().mockResolvedValue({ phase: 'idle', current_version: '0.5.1' }), applyUpdate: vi.fn(), checkForUpdate: vi.fn(), installUpdateStream: vi.fn(), uploadUpdate: vi.fn(),
}))

vi.mock('./update-reload', () => ({ scheduleFrontendReloadAfterUpdate: vi.fn(() => () => {}) }))

it('retains the install stream cause and correlation when returning to idle', async () => {
  vi.mocked(installUpdateStream).mockResolvedValue(new ReadableStream({ start(controller) {
    controller.enqueue({ event: 'error', data: JSON.stringify({
      message: 'Install failed', code: 'api.update.installFailed', request_id: 'update-request',
      details: { detail: 'download checksum mismatch', operation: 'update.install', backend_version: '0.5.0', platform: 'windows/amd64' },
    }) })
    controller.close()
  } }))
  const { result } = renderHook(() => useUpdateSettings())
  await waitFor(() => expect(result.current.operation).toBe('idle'))
  act(() => result.current.onInstall())
  await waitFor(() => expect(result.current.error).toContain('download checksum mismatch'))
  expect(result.current.error).toContain('update-request')
  expect(result.current.error).toContain('api.update.installFailed')
  expect(result.current.operation).toBe('idle')
  expect(result.current.installResult).toBeNull()
})

it('restores a staged update after remounting settings', async () => {
  vi.mocked(getUpdateStatus).mockResolvedValueOnce({ phase: 'staged', current_version: '0.5.1', version: '0.5.2', id: 'staged' })
  const { result, unmount } = renderHook(() => useUpdateSettings())
  await waitFor(() => expect(result.current.installResult).toMatchObject({ installed_version: '0.5.2', apply_ready: true }))
  expect(result.current.operation).toBe('idle')
  unmount()
})

it('reports a stream that ends before a result', async () => {
  vi.mocked(installUpdateStream).mockResolvedValueOnce(new ReadableStream({ start(controller) { controller.close() } }))
  const { result } = renderHook(() => useUpdateSettings())
  await waitFor(() => expect(result.current.operation).toBe('idle'))
  act(() => result.current.onInstall())
  await waitFor(() => expect(result.current.error).toBeTruthy())
  expect(result.current.installResult).toBeNull()
})

it('retains a retryable staged package after helper handoff fails', async () => {
  const staged = { phase: 'staged' as const, current_version: '0.5.1', version: '0.5.2', id: 'retry' }
  vi.mocked(getUpdateStatus).mockResolvedValueOnce(staged).mockResolvedValueOnce(staged)
  vi.mocked(applyUpdate).mockRejectedValueOnce(new Error('Helper could not start'))
    .mockResolvedValueOnce({ status: 'restarting', id: 'retry', version: '0.5.2' })
  const { result, unmount } = renderHook(() => useUpdateSettings())
  await waitFor(() => expect(result.current.installResult?.apply_ready).toBe(true))
  await act(async () => result.current.onApply())
  await waitFor(() => expect(result.current.error).toBe('Helper could not start'))
  expect(result.current.installResult?.apply_ready).toBe(true)
  expect(result.current.operation).toBe('idle')
  await act(async () => result.current.onApply())
  await waitFor(() => expect(result.current.operation).toBe('restarting'))
  expect(scheduleFrontendReloadAfterUpdate).toHaveBeenCalledWith({ status: 'restarting', id: 'retry', version: '0.5.2' }, expect.anything())
  unmount()
})

it('monitors an accepted update when the apply response is lost', async () => {
  vi.mocked(getUpdateStatus).mockResolvedValueOnce({ phase: 'staged', current_version: '0.5.1', version: '0.5.2', id: 'accepted' })
    .mockResolvedValueOnce({ phase: 'waiting', current_version: '0.5.1', version: '0.5.2', id: 'accepted' })
  vi.mocked(applyUpdate).mockRejectedValueOnce(new Error('Connection closed'))
  const { result, unmount } = renderHook(() => useUpdateSettings())
  await waitFor(() => expect(result.current.installResult?.apply_ready).toBe(true))
  act(() => result.current.onApply())
  await waitFor(() => expect(result.current.operation).toBe('restarting'))
  expect(result.current.installResult).toBeNull()
  expect(scheduleFrontendReloadAfterUpdate).toHaveBeenCalledWith({ id: 'accepted', version: '0.5.2' }, expect.anything())
  unmount()
})

it('restores the staged package if the install stream loses its result', async () => {
  vi.mocked(getUpdateStatus).mockResolvedValueOnce({ phase: 'idle', current_version: '0.5.1' })
    .mockResolvedValueOnce({ phase: 'staged', current_version: '0.5.1', version: '0.5.2', id: 'downloaded' })
  vi.mocked(installUpdateStream).mockResolvedValueOnce(new ReadableStream({ start(controller) { controller.close() } }))
  const { result, unmount } = renderHook(() => useUpdateSettings())
  await waitFor(() => expect(result.current.operation).toBe('idle'))
  act(() => result.current.onInstall())
  await waitFor(() => expect(result.current.installResult).toMatchObject({ apply_ready: true, installed_version: '0.5.2' }))
  unmount()
})

it('keeps a restart timeout visible instead of restarting its deadline on each status poll', async () => {
  vi.useFakeTimers()
  vi.mocked(scheduleFrontendReloadAfterUpdate).mockClear()
  vi.mocked(getUpdateStatus).mockResolvedValue({ phase: 'waiting', current_version: '0.5.1', version: '0.5.2', id: 'slow-update' })
  const { result, unmount } = renderHook(() => useUpdateSettings())
  try {
    await act(async () => {})
    expect(scheduleFrontendReloadAfterUpdate).toHaveBeenCalledOnce()
    act(() => vi.mocked(scheduleFrontendReloadAfterUpdate).mock.calls[0][1].onError('timeout'))
    const timeout = result.current.error
    expect(timeout).toBeTruthy()
    await act(async () => { await vi.advanceTimersByTimeAsync(4000) })
    expect(result.current.error).toBe(timeout)
    expect(scheduleFrontendReloadAfterUpdate).toHaveBeenCalledOnce()
    vi.mocked(getUpdateStatus).mockResolvedValue({ phase: 'staged', current_version: '0.5.1', version: '0.5.2', id: 'slow-update' })
    await act(async () => { await vi.advanceTimersByTimeAsync(2000) })
    vi.mocked(applyUpdate).mockResolvedValueOnce({ status: 'restarting', version: '0.5.2', id: 'slow-update' })
    await act(async () => result.current.onApply())
    expect(scheduleFrontendReloadAfterUpdate).toHaveBeenCalledTimes(2)
    expect(result.current.operation).toBe('restarting')
    expect(result.current.error).toBeNull()
  } finally {
    unmount()
    vi.useRealTimers()
    vi.mocked(getUpdateStatus).mockResolvedValue({ phase: 'idle', current_version: '0.5.1' })
  }
})

it('clears a transient polling failure once the staged update can be read again', async () => {
  vi.useFakeTimers()
  vi.mocked(getUpdateStatus).mockRejectedValueOnce(new Error('Backend unavailable'))
    .mockResolvedValue({ phase: 'staged', current_version: '0.5.1', version: '0.5.2', id: 'recovered' })
  const { result, unmount } = renderHook(() => useUpdateSettings())
  try {
    await act(async () => {})
    expect(result.current.error).toBe('Backend unavailable')
    await act(async () => { await vi.advanceTimersByTimeAsync(2000) })
    expect(result.current.installResult?.apply_ready).toBe(true)
    expect(result.current.error).toBeNull()
  } finally {
    unmount()
    vi.useRealTimers()
    vi.mocked(getUpdateStatus).mockResolvedValue({ phase: 'idle', current_version: '0.5.1' })
  }
})
