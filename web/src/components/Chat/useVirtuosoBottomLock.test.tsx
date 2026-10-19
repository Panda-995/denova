import { act, renderHook } from '@testing-library/react'
import type { PointerEvent, WheelEvent } from 'react'
import { expect, it, vi } from 'vitest'
import type { VirtuosoHandle } from 'react-virtuoso'
import { useVirtuosoBottomLock } from './useVirtuosoBottomLock'

it('keeps restored history at the bottom while long rows are measured before a result footer', () => {
  const scroller = document.createElement('div')
  Object.defineProperty(scroller, 'clientHeight', { value: 500 })
  Object.defineProperty(scroller, 'scrollHeight', { value: 1000, configurable: true })
  scroller.scrollTop = 500
  const { result } = renderHook(() => useVirtuosoBottomLock({
    itemCount: 6, autoFollowEnabled: false, revealAfterContent: true, resolveScroller: () => scroller,
  }))
  for (const height of [1500, 300_000, 300_500]) {
    Object.defineProperty(scroller, 'scrollHeight', { value: height, configurable: true })
    act(() => result.current.syncIdleBottomLayout())
    expect(scroller.scrollTop).toBe(height - 500)
  }
  act(() => result.current.onWheel({ deltaY: -100 } as WheelEvent<HTMLDivElement>))
  // Virtuoso may report the old bottom before the browser applies the wheel.
  act(() => result.current.onAtBottomStateChange(true))
  scroller.scrollTop = 200_000
  Object.defineProperty(scroller, 'scrollHeight', { value: 301_000, configurable: true })
  act(() => result.current.syncIdleBottomLayout())
  expect(scroller.scrollTop).toBe(200_000)
})

it('preserves the completed reply until the reader explicitly returns to the bottom', () => {
  const scroller = document.createElement('div')
  Object.defineProperty(scroller, 'clientHeight', { value: 500 })
  Object.defineProperty(scroller, 'scrollHeight', { value: 1000, configurable: true })
  scroller.scrollTop = 500
  const { result, rerender } = renderHook(({ streaming }) => useVirtuosoBottomLock({
    itemCount: 6, autoFollowEnabled: streaming, revealAfterContent: true, resolveScroller: () => scroller,
  }), { initialProps: { streaming: false } })
  rerender({ streaming: true })
  rerender({ streaming: false })
  Object.defineProperty(scroller, 'scrollHeight', { value: 2000, configurable: true })
  act(() => result.current.syncIdleBottomLayout())
  expect(scroller.scrollTop).toBe(500)
  act(() => result.current.scrollToBottom())
  act(() => result.current.syncIdleBottomLayout())
  expect(scroller.scrollTop).toBe(1500)
})

it('materializes a streamed tail outside the virtual range without overriding an upward scroll', () => {
  const scroller = document.createElement('div')
  Object.defineProperty(scroller, 'clientHeight', { value: 500 })
  Object.defineProperty(scroller, 'scrollHeight', { value: 1000 })
  const resolveScroller = () => scroller
  const { result, rerender } = renderHook(({ count }) => useVirtuosoBottomLock({ itemCount: count, autoFollowEnabled: true, resolveScroller }), { initialProps: { count: 3 } })
  const scrollToIndex = vi.fn()
  result.current.virtuosoRef.current = { scrollToIndex } as unknown as VirtuosoHandle
  rerender({ count: 1000 })
  expect(scrollToIndex).toHaveBeenCalledWith({ index: 'LAST', align: 'end', behavior: 'auto' })
  scrollToIndex.mockClear()
  act(() => result.current.onWheel({ deltaY: -100 } as WheelEvent<HTMLDivElement>))
  rerender({ count: 1001 })
  expect(scrollToIndex).not.toHaveBeenCalled()
})

it('follows footer expansion from the bottom throughout layout updates', () => {
  const scroller = document.createElement('div')
  Object.defineProperty(scroller, 'clientHeight', { value: 500 })
  Object.defineProperty(scroller, 'scrollHeight', { value: 1000, configurable: true })
  scroller.scrollTop = 500
  const { result } = renderHook(() => useVirtuosoBottomLock({ itemCount: 3, autoFollowEnabled: false, resolveScroller: () => scroller }))
  act(() => { result.current.beginAfterContentInteraction(); result.current.releaseBottomLock() })
  for (const height of [1100, 1250, 1400, 1000]) {
    Object.defineProperty(scroller, 'scrollHeight', { value: height, configurable: true })
    act(() => result.current.restoreAfterContentScrollPosition())
    expect(scroller.scrollTop).toBe(height - 500)
  }
})

it('preserves the reading position when expansion starts away from the bottom', () => {
  const scroller = document.createElement('div')
  Object.defineProperty(scroller, 'clientHeight', { value: 500 })
  Object.defineProperty(scroller, 'scrollHeight', { value: 1000, configurable: true })
  scroller.scrollTop = 200
  const { result } = renderHook(() => useVirtuosoBottomLock({ itemCount: 3, autoFollowEnabled: false, resolveScroller: () => scroller }))
  act(() => result.current.beginAfterContentInteraction())
  Object.defineProperty(scroller, 'scrollHeight', { value: 1400, configurable: true })
  act(() => result.current.restoreAfterContentScrollPosition())
  expect(scroller.scrollTop).toBe(200)
})

it.each(['wheel', 'touch'] as const)('stops following when the user scrolls upward with %s during expansion', (input) => {
  const scroller = document.createElement('div')
  Object.defineProperty(scroller, 'clientHeight', { value: 500 })
  Object.defineProperty(scroller, 'scrollHeight', { value: 1000, configurable: true })
  scroller.scrollTop = 500
  const { result } = renderHook(() => useVirtuosoBottomLock({ itemCount: 3, autoFollowEnabled: false, resolveScroller: () => scroller }))
  act(() => result.current.beginAfterContentInteraction())
  if (input === 'wheel') {
    act(() => result.current.onWheel({ deltaY: -100 } as WheelEvent<HTMLDivElement>))
  } else {
    const footer = document.createElement('div')
    footer.setAttribute('data-nova-chat-after-content', '')
    scroller.append(footer)
    act(() => result.current.onPointerDown({ target: footer, currentTarget: scroller, pointerType: 'touch', clientY: 200 } as unknown as PointerEvent<HTMLDivElement>))
    act(() => result.current.onPointerMove({ clientY: 220 } as PointerEvent<HTMLDivElement>))
  }
  scroller.scrollTop = 400
  Object.defineProperty(scroller, 'scrollHeight', { value: 1400, configurable: true })
  act(() => result.current.restoreAfterContentScrollPosition())
  expect(scroller.scrollTop).toBe(400)
})
