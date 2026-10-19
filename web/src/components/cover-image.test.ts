import { expect, it } from 'vitest'
import { coverPosition } from './cover-image'

it('centers default crops for landscape and portrait containers', () => {
  expect(coverPosition(400, 200, 100, 100)).toBe('50% 50%')
  expect(coverPosition(200, 400, 100, 100)).toBe('50% 50%')
})

it('centers the source focal point and clamps to keep the viewport covered', () => {
  expect(coverPosition(200, 200, 400, 200, { x: 0.375, y: 0.8 })).toBe('25% 50%')
  expect(coverPosition(200, 200, 400, 200, { x: 0, y: 0 })).toBe('0% 50%')
  expect(coverPosition(200, 200, 400, 200, { x: 1, y: 1 })).toBe('100% 50%')
  expect(coverPosition(400, 200, 400, 400, { x: 0.2, y: 0.625 })).toBe('50% 75%')
  expect(coverPosition(400, 200, 400, 200, { x: 0, y: 1 })).toBe('50% 50%')
})
