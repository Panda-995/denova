import type { ActorStateField } from '../../types'

interface NumberMeter {
  widthPercent: number
  tone: 'standard' | 'negative' | 'positive' | 'neutral'
}

/** Signed ranges show magnitude from the left; color carries the sign. */
export function resolveNumberMeter(field: ActorStateField, value: unknown, capacity?: number): NumberMeter | null {
  const min = field.min ?? (field.max_field ? 0 : undefined)
  const max = field.max_field ? capacity : field.max
  if (typeof value !== 'number' || !Number.isFinite(value)
    || typeof min !== 'number' || !Number.isFinite(min)
    || typeof max !== 'number' || !Number.isFinite(max)
    || max <= min) return null

  if (min < 0 && max > 0) {
    return {
      widthPercent: Math.min(100, (value < 0 ? value / min : value / max) * 100),
      tone: value < 0 ? 'negative' : value > 0 ? 'positive' : 'neutral',
    }
  }
  return { widthPercent: numberPosition(value, min, max), tone: 'standard' }
}

function numberPosition(value: number, min: number, max: number) {
  return Math.min(100, Math.max(0, ((value - min) / (max - min)) * 100))
}
