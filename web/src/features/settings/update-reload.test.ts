import { afterEach, expect, it, vi } from 'vitest'
import { scheduleFrontendReloadAfterUpdate, UPDATE_RELOAD_TIMEOUT_MS } from './update-reload'
import type { UpdateStatus } from './types'

afterEach(() => vi.useRealTimers())

it('waits for the matching transaction and running version before refreshing', async () => {
  vi.useFakeTimers()
  const reload = vi.fn(), onError = vi.fn()
  const pollBackend = vi.fn<() => Promise<UpdateStatus>>()
    .mockResolvedValueOnce({ id: 'update', phase: 'waiting', current_version: '0.5.1' })
    .mockResolvedValueOnce({ id: 'other-update', phase: 'succeeded', current_version: '0.5.2' })
    .mockResolvedValueOnce({ id: 'update', phase: 'succeeded', current_version: '0.5.1' })
    .mockResolvedValue({ id: 'update', phase: 'succeeded', current_version: '0.5.2' })
  const cancel = scheduleFrontendReloadAfterUpdate({ id: 'update', version: 'v0.5.2' }, { pollBackend, reload, onError, currentHref: 'http://localhost/settings?language=zh' })
  await vi.advanceTimersByTimeAsync(3000)
  expect(reload).not.toHaveBeenCalled()
  await vi.advanceTimersByTimeAsync(1000)
  expect(reload).toHaveBeenCalledOnce()
  expect(reload.mock.calls[0][0]).toContain('language=zh&denova_reload=v0.5.2-')
  expect(onError).not.toHaveBeenCalled()
  cancel()
})

it('stops on a durable failure, timeout, or unmount', async () => {
  vi.useFakeTimers()
  const reload = vi.fn(), onError = vi.fn()
  const pollBackend = vi.fn<() => Promise<UpdateStatus>>().mockResolvedValue({ id: 'update', phase: 'failed', current_version: '0.5.1', log_path: 'apply.log' })
  const target = { id: 'update', version: '0.5.2' }
  scheduleFrontendReloadAfterUpdate(target, { pollBackend, reload, onError })
  await vi.advanceTimersByTimeAsync(2000)
  expect(onError).toHaveBeenCalledWith('failed', expect.objectContaining({ log_path: 'apply.log' }))
  expect(pollBackend).toHaveBeenCalledTimes(1)
  onError.mockClear()
  pollBackend.mockRejectedValue(new Error('connection refused'))
  scheduleFrontendReloadAfterUpdate(target, { pollBackend, reload, onError })
  await vi.advanceTimersByTimeAsync(UPDATE_RELOAD_TIMEOUT_MS + 1000)
  expect(onError).toHaveBeenCalledWith('timeout')
  onError.mockClear()
  const cancel = scheduleFrontendReloadAfterUpdate(target, { pollBackend, reload, onError })
  cancel()
  await vi.advanceTimersByTimeAsync(UPDATE_RELOAD_TIMEOUT_MS)
  expect(onError).not.toHaveBeenCalled()
  expect(reload).not.toHaveBeenCalled()
})
