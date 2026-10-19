// Speech, media previews and scene ambience share one audible owner per page.
// Ownership changes before pausing the previous player so its cleanup cannot
// release the new owner's focus. A displaced player never resumes itself.
let pauseCurrent: (() => void) | undefined

export function claimAudioFocus(pause: () => void) {
  const previous = pauseCurrent
  pauseCurrent = pause
  if (previous !== pause) previous?.()
}

export function releaseAudioFocus(pause: () => void) {
  if (pauseCurrent === pause) pauseCurrent = undefined
}
