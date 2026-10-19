import type { CSSProperties } from 'react'
import type { StageCharacterLayout } from '@/features/settings/types'
import { useEffect, useState } from 'react'
import { CoverImage } from '@/components/cover-image'
import { projectFileAssetURL } from '@/lib/api-client/project-files'
import { visibleStoryPresentation } from '../../presentation'
import type { PresentationMaterial, StoryPresentationSettings, TurnEvent } from '../../types'

interface StoryStageArtworkProps {
  projectId: string
  turn?: TurnEvent
  // Derived from the branch's ordered turns. parent_id may identify a plan or
  // other journal event, so it cannot establish visual turn continuity.
  previousTurnId?: string
  latest: boolean
  settings?: StoryPresentationSettings
  textHidden: boolean
  characterLayout?: StageCharacterLayout
  characterSize?: number
  scrimOpacity: number
}

// Key this component by project/story/branch. Only an immediate continuation at
// the live head may retain a previously loaded image while its replacement loads.
export function StoryStageArtwork({ projectId, turn, previousTurnId, latest, settings, textHidden, scrimOpacity, characterLayout = 'center', characterSize = 0.7 }: StoryStageArtworkProps) {
  const scene = `${turn?.id || ''}:${turn?.version_idx ?? 0}`
  const [continuity, setContinuity] = useState({ scene, turnId: turn?.id, latest, epoch: 0 })
  if (continuity.scene !== scene || continuity.latest !== latest) {
    const continues = latest && continuity.latest && previousTurnId !== undefined && previousTurnId === continuity.turnId
    setContinuity({ scene, turnId: turn?.id, latest, epoch: continuity.epoch + (continues ? 0 : 1) })
  }
  const { background, characters = [] } = visibleStoryPresentation(turn?.turn_result?.presentation, settings)
  // Initial background changes must not retain a different image if loading
  // fails. Committed scenes share the live background slot across turns.
  const backgroundSlot = turn?.turn_result?.presentation ? 'turn' : `default:${background?.path}`
  if (!background && !characters.length) return null

  return (
    <div data-testid="story-stage-artwork" className="nova-story-artwork pointer-events-none absolute inset-0 overflow-hidden" aria-hidden="true">
      {background && <StageImage fallbackKey={`${continuity.epoch}:${backgroundSlot}`} projectId={projectId} material={background} layer="background" />}
      <div className="nova-stage-characters" data-layout={characterLayout} style={{ '--character-height': `${characterSize * 100}%` } as CSSProperties}>
        {characters.map((character, index) => {
          // Wide stages use stable list positions; narrow stages retain cast order.
          const side = characterLayout === 'sides' ? (characters.length === 1 || index % 2 === 1 ? 'right' : 'left') : characterLayout
          const count = characterLayout === 'sides'
            ? (side === 'left' ? Math.ceil(characters.length / 2) : Math.max(1, Math.floor(characters.length / 2)))
            : characters.length
          const sideIndex = characterLayout === 'sides' ? Math.floor(index / 2) : index
          return (
            <div key={character.item_id} className="nova-stage-character" data-side={side}
              style={{
                '--cast-position': characters.length === 1 ? 0.5 : index / (characters.length - 1),
                '--side-position': count === 1 ? (side === 'right' ? 1 : 0) : sideIndex / (count - 1),
              } as CSSProperties}>
              <StageImage fallbackKey={String(continuity.epoch)} projectId={projectId} material={character} layer="character" />
            </div>
          )
        })}
      </div>
      <div data-testid="story-stage-scrim" className="absolute inset-0 bg-[var(--nova-surface-2)] transition-opacity duration-200 motion-reduce:transition-none" style={{ opacity: textHidden ? 0 : scrimOpacity }} />
    </div>
  )
}

function StageImage({ projectId, material, layer, fallbackKey }: { projectId: string; material: PresentationMaterial; layer: 'background' | 'character'; fallbackKey: string }) {
  const src = projectFileAssetURL(projectId, material.path)
  const [imageState, setImageState] = useState<{ fallbackKey: string; loaded?: { src: string; name: string; focus?: PresentationMaterial['focus'] } }>({ fallbackKey })
  if (imageState.fallbackKey !== fallbackKey) {
    // Reset stale fallbacks without remounting the same asset during history scrolling.
    setImageState({ fallbackKey, loaded: imageState.loaded?.src === src ? imageState.loaded : undefined })
  }
  const { loaded } = imageState
  useEffect(() => {
    let cancelled = false
    const image = new Image()
    image.onload = () => {
      // decode avoids replacing the previous image before the browser can paint.
      const ready = typeof image.decode === 'function' ? image.decode() : Promise.resolve()
      void ready.then(() => { if (!cancelled) setImageState(current => ({ ...current, loaded: { src, name: material.name, focus: material.focus } })) }).catch(() => {
        if (!cancelled) console.warn('[story-presentation] image decoding failed', { path: material.path })
      })
    }
    image.onerror = () => console.warn('[story-presentation] image loading failed', { path: material.path })
    image.src = src
    return () => { cancelled = true; image.onload = null; image.onerror = null }
  }, [src, material.name, material.path, material.focus])

  if (!loaded) return null
  const className = 'absolute inset-0 h-full w-full animate-in fade-in duration-200 motion-reduce:animate-none'
  return layer === 'background'
    ? <CoverImage data-stage-layer={layer} src={loaded.src} alt={loaded.name} focus={loaded.src === src ? material.focus : loaded.focus} draggable={false} className={className} />
    : <img data-stage-layer={layer} src={loaded.src} alt={loaded.name} draggable={false} className="h-full w-auto max-w-full object-contain object-bottom animate-in fade-in duration-200 motion-reduce:animate-none" />
}
