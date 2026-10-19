import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react'
import { Music2, Pause, Play, Volume2, VolumeX } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Slider } from '@/components/ui/slider'
import { projectFileAssetURL } from '@/lib/api-client/project-files'
import { claimAudioFocus, releaseAudioFocus } from '@/features/speech/audio-focus'
import type { PresentationMaterial } from '../../types'

type PlaybackStatus = 'paused' | 'loading' | 'playing' | 'blocked' | 'error'

function savedVolume() {
  try {
    const stored = localStorage.getItem('denova:ambient-volume')
    const volume = stored === null ? 0.25 : Number(stored)
    return Number.isFinite(volume) ? Math.max(0, Math.min(1, volume)) : 0.25
  } catch { return 0.25 }
}

/** One ephemeral loop follows the committed continuation point, never scroll
 * position. Replacements retain playback intent only along ordinary next turns. */
export function StoryAmbientPlayback({ projectId, material, turnId, previousTurnId, active }: {
  projectId: string
  material?: PresentationMaterial
  turnId?: string
  previousTurnId?: string
  active: boolean
}) {
  const { t } = useTranslation()
  const [status, setStatus] = useState<PlaybackStatus>('paused')
  const [volume, setVolume] = useState(savedVolume)
  const [muted, setMuted] = useState(false)
  const audio = useRef<HTMLAudioElement | null>(null)
  const wanted = useRef(false)
  const playAttempt = useRef(0)
  const preferences = useRef({ volume, muted })
  const previousTurn = useRef(turnId)
  const src = material?.path ? projectFileAssetURL(projectId, material.path) : ''

  const pause = useCallback(() => {
    playAttempt.current++
    wanted.current = false
    audio.current?.pause()
    releaseAudioFocus(pause)
    setStatus('paused')
  }, [])

  const play = useCallback(async () => {
    const element = audio.current
    if (!element) return
    // A paused or retried play() may settle after the next attempt on the same
    // element. Its promise must not change the new playback's state or focus.
    const attempt = ++playAttempt.current
    wanted.current = true
    claimAudioFocus(pause)
    setStatus('loading')
    if (element.error) element.load()
    try {
      await element.play()
      if (element !== audio.current || attempt !== playAttempt.current) return
      if (!wanted.current) element.pause()
      else setStatus('playing')
    } catch (error) {
      if (element !== audio.current || attempt !== playAttempt.current || !wanted.current) return
      wanted.current = false
      releaseAudioFocus(pause)
      const blocked = error instanceof DOMException && error.name === 'NotAllowedError'
      setStatus(blocked ? 'blocked' : 'error')
      console.warn('[story-ambient] playback failed', { reason: blocked ? 'autoplay blocked' : 'media unavailable' })
    }
  }, [pause])

  useLayoutEffect(() => {
    preferences.current = { volume, muted }
    if (audio.current) { audio.current.volume = volume; audio.current.muted = muted }
  }, [volume, muted])

  useLayoutEffect(() => {
    if (!active || (previousTurn.current !== turnId && previousTurn.current !== previousTurnId)) pause()
    previousTurn.current = turnId
  }, [active, turnId, previousTurnId, pause])

  useEffect(() => {
    if (!src) { pause(); return }
    const element = new Audio()
    element.preload = 'none'
    element.loop = true
    element.volume = preferences.current.volume
    element.muted = preferences.current.muted
    element.src = src
    audio.current = element
    element.onplaying = () => {
      if (wanted.current) setStatus('playing')
      else element.pause()
    }
    element.onwaiting = () => { if (wanted.current) setStatus('loading') }
    element.onpause = () => setStatus('paused')
    element.onerror = () => {
      wanted.current = false
      releaseAudioFocus(pause)
      setStatus('error')
      console.warn('[story-ambient] audio loading failed', { path: src })
    }
    if (wanted.current) void play()
    else setStatus('paused')
    return () => {
      playAttempt.current++
      element.onplaying = element.onwaiting = element.onpause = element.onerror = null
      element.pause()
      element.removeAttribute('src')
      element.load()
      audio.current = null
      releaseAudioFocus(pause)
    }
  }, [src, pause, play])

  useEffect(() => {
    window.addEventListener('pagehide', pause)
    return () => window.removeEventListener('pagehide', pause)
  }, [pause])

  if (!material || !active) return null
  const running = status === 'playing' || status === 'loading'
  return (
    <div className="flex min-w-0 flex-wrap items-center gap-2 rounded-md border border-border bg-card px-3 py-2 text-xs text-foreground" data-testid="story-ambient-playback" data-status={status}>
      <Music2 className="size-4 shrink-0 text-muted-foreground" aria-hidden="true" />
      <div className="flex min-w-0 flex-1 flex-col gap-1" role="status" aria-live="polite">
        <span className="truncate" title={material.name}>{material.name}</span>
        <span className="text-muted-foreground">{t(`storyStage.ambient.${status}`)}</span>
      </div>
      <Button variant="ghost" size="icon-sm" aria-label={t(running ? 'storyStage.ambient.pause' : 'storyStage.ambient.play')} onClick={() => running ? pause() : void play()}>
        {running ? <Pause /> : <Play />}
      </Button>
      <Button variant="ghost" size="icon-sm" aria-label={t('storyStage.ambient.mute')} aria-pressed={muted} onClick={() => setMuted(!muted)}>
        {muted || volume === 0 ? <VolumeX /> : <Volume2 />}
      </Button>
      <Slider className="min-w-16 max-w-24 flex-1" aria-label={t('storyStage.ambient.volume')} aria-valuetext={`${Math.round(volume * 100)}%`} value={[volume]} min={0} max={1} step={0.05} onValueChange={([value]) => {
        setVolume(value)
        try { localStorage.setItem('denova:ambient-volume', String(value)) } catch { /* Playback does not require storage. */ }
      }} />
    </div>
  )
}
