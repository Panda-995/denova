import type { StoryPresentationSettings, TurnPresentation } from './types'

// A committed presentation can explicitly clear its background. Only an absent
// snapshot uses the opening value. The dynamic switch only controls Agent edits.
export function visibleStoryPresentation(stage?: TurnPresentation, settings?: StoryPresentationSettings): TurnPresentation {
  return {
    background: stage ? stage.background : settings?.default_background,
    characters: settings?.characters !== false ? stage?.characters : undefined,
  }
}
