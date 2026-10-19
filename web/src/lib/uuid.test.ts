import { afterEach, describe, expect, it, vi } from 'vitest'
import { randomUUID } from './uuid'

afterEach(() => vi.unstubAllGlobals())

describe('browser UUIDs', () => {
  it('uses the native implementation in secure contexts', () => {
    const native = vi.fn(() => 'b3afae52-9047-49de-8d27-b5b968d91538')
    vi.stubGlobal('crypto', { randomUUID: native })
    expect(randomUUID()).toBe('b3afae52-9047-49de-8d27-b5b968d91538')
    expect(native).toHaveBeenCalledOnce()
  })

  it.each([0x00, 0xff])('preserves random bytes and sets UUID v4 bits over HTTP (%i)', (byte) => {
    const getRandomValues = vi.fn((bytes: Uint8Array) => bytes.fill(byte))
    vi.stubGlobal('crypto', { getRandomValues })
    expect(randomUUID()).toBe(byte === 0
      ? '00000000-0000-4000-8000-000000000000'
      : 'ffffffff-ffff-4fff-bfff-ffffffffffff')
    expect(getRandomValues).toHaveBeenCalledOnce()
    expect(getRandomValues.mock.calls[0][0]).toHaveLength(16)
  })
})
