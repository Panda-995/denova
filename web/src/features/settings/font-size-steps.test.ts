import { describe, expect, it } from 'vitest'
import {
  DEFAULT_READING_FONT_SIZE,
  DEFAULT_UI_FONT_SIZE,
  nearestFontSizeStepIndex,
  READING_FONT_SIZE_STEPS,
  UI_FONT_SIZE_STEPS,
} from './font-size-steps'

describe('font size steps', () => {
  it('maps released free-form values to the nearest discrete step without changing storage', () => {
    expect(nearestFontSizeStepIndex(22, READING_FONT_SIZE_STEPS, DEFAULT_READING_FONT_SIZE)).toBe(5)
    expect(nearestFontSizeStepIndex(17, UI_FONT_SIZE_STEPS, DEFAULT_UI_FONT_SIZE)).toBe(5)
    expect(nearestFontSizeStepIndex(null, UI_FONT_SIZE_STEPS, DEFAULT_UI_FONT_SIZE)).toBe(3)
  })
})
