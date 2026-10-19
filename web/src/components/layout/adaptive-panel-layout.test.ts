import { describe, expect, it } from 'vitest'
import { adaptivePanelMinimumWidth, resolveAdaptivePanelLayout, type AdaptivePanelSizing } from './adaptive-panel-layout'

const sizing: AdaptivePanelSizing = {
  left: { layoutKey: 'navigation', label: '', minSize: '200px', maxSize: '40%', mainMinSize: '320px' },
  right: { layoutKey: 'content', label: '', minSize: '280px', maxSize: '75%', mainMinSize: '360px' },
  leftVisible: true, rightVisible: true, rightExpanded: false,
}

function pixels(config: AdaptivePanelSizing, width: number, left: number, ratio: number) {
  const result = resolveAdaptivePanelLayout(config, width, left, ratio)!
  return Object.fromEntries(Object.entries(result.layout).map(([key, size]) => [key, Math.round(size * width / 100)]))
}

describe('adaptive three-pane allocation', () => {
  it('shares space by the content ratio when both panes have room', () => {
    expect(pixels({ ...sizing, leftVisible: false }, 1000, 200, 0.4)).toEqual({ left: 0, main: 600, right: 400 })
    expect(pixels(sizing, 1000, 200, 0.4)).toEqual({ left: 200, main: 480, right: 320 })
  })

  it('freezes either content pane at its minimum while the other absorbs the deficit', () => {
    expect(pixels(sizing, 1000, 200, 0.64)).toEqual({ left: 200, main: 360, right: 440 })
    expect(pixels(sizing, 1000, 200, 0.28)).toEqual({ left: 200, main: 520, right: 280 })
  })

  it('clamps navigation to leave room for both content minima and rejects an impossible split', () => {
    expect(pixels(sizing, 900, 350, 0.5)).toEqual({ left: 260, main: 360, right: 280 })
    expect(adaptivePanelMinimumWidth(sizing)).toBe(840)
    expect(resolveAdaptivePanelLayout(sizing, 839, 260, 0.5)).toBeNull()
  })

  it('uses the content area as the percentage maximum', () => {
    expect(pixels(sizing, 1800, 200, 0.9)).toEqual({ left: 200, main: 400, right: 1200 })
  })

  it('maximizes and closes the right pane without changing the navigation preference', () => {
    expect(pixels({ ...sizing, rightExpanded: true }, 1000, 260, 0.4)).toEqual({ left: 260, main: 0, right: 740 })
    expect(pixels({ ...sizing, rightVisible: false }, 1000, 260, 0.4)).toEqual({ left: 260, main: 740, right: 0 })
    expect(pixels(sizing, 1000, 260, 0.4)).toEqual({ left: 260, main: 444, right: 296 })
  })
})
