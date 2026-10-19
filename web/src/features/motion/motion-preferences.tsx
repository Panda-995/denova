import { createContext, useContext, useEffect } from 'react'
import type { ReactNode } from 'react'
import { MotionConfig, useReducedMotion } from 'motion/react'

export type MotionIntensity = 'system' | 'full' | 'reduced' | 'off'

const MotionIntensityContext = createContext<MotionIntensity>('system')

/** Shares the normalized preference with renderers whose animations add DOM work. */
export function useMotionIntensity(): MotionIntensity {
  return useContext(MotionIntensityContext)
}

export function normalizeMotionIntensity(value?: string | null): MotionIntensity {
  if (value === 'full' || value === 'reduced' || value === 'off' || value === 'system') return value
  return 'system'
}

export function NovaMotionProvider({
  intensity,
  children,
}: {
  intensity?: string | null
  children: ReactNode
}) {
  const normalized = normalizeMotionIntensity(intensity)
  const systemReduced = useReducedMotion()
  const disabled = normalized === 'off'
  const reduced = disabled || normalized === 'reduced' || (normalized === 'system' && Boolean(systemReduced))
  const reducedMotion = normalized === 'full' ? 'never' : (reduced ? 'always' : 'user')

  useEffect(() => {
    if (typeof document === 'undefined') return
    document.documentElement.dataset.novaMotion = normalized
  }, [normalized])

  return (
    <MotionIntensityContext.Provider value={normalized}>
      <MotionConfig reducedMotion={reducedMotion}>
        {children}
      </MotionConfig>
    </MotionIntensityContext.Provider>
  )
}
