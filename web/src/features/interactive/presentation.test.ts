import { expect, it } from 'vitest'
import { visibleStoryPresentation } from './presentation'

it('uses the opening background only before a stage exists, regardless of Agent control', () => {
  const opening = { item_id: 'room', asset_id: 'day', path: 'assets/day.png', name: 'Day' }
  const current = { ...opening, asset_id: 'night', path: 'assets/night.png', name: 'Night' }
  for (const background of [true, false]) {
    const settings = { background, characters: true, default_background: opening }
    expect(visibleStoryPresentation(undefined, settings).background).toEqual(opening)
    expect(visibleStoryPresentation({ background: current }, settings).background).toEqual(current)
    expect(visibleStoryPresentation({}, settings).background).toBeUndefined()
  }
})
