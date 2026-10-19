import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Crosshair } from 'lucide-react'
import { CoverImage, type ImageFocus } from '@/components/cover-image'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Field, FieldLabel, FieldGroup } from '@/components/ui/field'
import { Slider } from '@/components/ui/slider'

export function BackgroundFocusDialog({ src, focus, disabled, onClose, onSave }: {
  src: string
  focus?: ImageFocus
  disabled: boolean
  onClose: () => void
  onSave: (focus?: ImageFocus) => void
}) {
  const { t } = useTranslation()
  const [draft, setDraft] = useState(focus ?? { x: 0.5, y: 0.5 })
  const [loaded, setLoaded] = useState(false)
  const [failed, setFailed] = useState(false)
  return <Dialog open onOpenChange={open => { if (!open) onClose() }}>
    <DialogContent className="max-h-[90dvh] overflow-y-auto">
      <DialogHeader>
        <DialogTitle>{t('storyStage.presentation.focus')}</DialogTitle>
        <DialogDescription>{t('storyStage.presentation.focusHelp')}</DialogDescription>
      </DialogHeader>
      <div className="flex justify-center rounded-md bg-muted p-3">
        <button type="button" disabled={disabled || !loaded} aria-label={t('storyStage.presentation.focusPoint')} className="relative block max-w-full touch-none cursor-crosshair focus-visible:outline-2 focus-visible:outline-ring" onPointerDown={event => {
          event.currentTarget.setPointerCapture(event.pointerId)
          const rect = event.currentTarget.getBoundingClientRect()
          setDraft({ x: Math.max(0, Math.min(1, (event.clientX - rect.left) / rect.width)), y: Math.max(0, Math.min(1, (event.clientY - rect.top) / rect.height)) })
        }} onPointerMove={event => {
          if (!event.currentTarget.hasPointerCapture(event.pointerId)) return
          const rect = event.currentTarget.getBoundingClientRect()
          setDraft({ x: Math.max(0, Math.min(1, (event.clientX - rect.left) / rect.width)), y: Math.max(0, Math.min(1, (event.clientY - rect.top) / rect.height)) })
        }}>
          <img src={src} alt="" draggable={false} className="block max-h-[22dvh] max-w-full" onLoad={() => setLoaded(true)} onError={() => { setFailed(true); console.warn('[story-presentation] focus image failed to load') }} />
          {loaded && <Crosshair aria-hidden className="pointer-events-none absolute size-6 -translate-x-1/2 -translate-y-1/2 rounded-full bg-background text-foreground shadow" style={{ left: `${draft.x * 100}%`, top: `${draft.y * 100}%` }} />}
        </button>
      </div>
      {failed && <p role="alert" className="text-sm text-destructive">{t('lore.materials.imageFailed')}</p>}
      <FieldGroup>
        {(['x', 'y'] as const).map(axis => <Field key={axis}>
          <FieldLabel>{t(`storyStage.presentation.focus${axis === 'x' ? 'Horizontal' : 'Vertical'}`)} · {Math.round(draft[axis] * 100)}%</FieldLabel>
          <Slider min={0} max={100} step={1} disabled={disabled || !loaded} value={[Math.round(draft[axis] * 100)]} aria-label={t(`storyStage.presentation.focus${axis === 'x' ? 'Horizontal' : 'Vertical'}`)} onValueChange={([value]) => setDraft(current => ({ ...current, [axis]: value / 100 }))} />
        </Field>)}
      </FieldGroup>
      <div className="mx-auto flex w-full max-w-64 items-start gap-3">
        <figure className="min-w-0 flex-[3]">
          <CoverImage src={src} focus={draft} alt="" className="aspect-video w-full rounded-md" />
          <figcaption className="mt-1 text-xs text-muted-foreground">{t('storyStage.presentation.focusWide')}</figcaption>
        </figure>
        <figure className="min-w-0 flex-1">
          <CoverImage src={src} focus={draft} alt="" className="aspect-[9/16] w-full rounded-md" />
          <figcaption className="mt-1 text-xs text-muted-foreground">{t('storyStage.presentation.focusTall')}</figcaption>
        </figure>
      </div>
      <DialogFooter>
        <Button variant="outline" disabled={disabled} onClick={() => setDraft({ x: 0.5, y: 0.5 })}>{t('storyStage.presentation.focusReset')}</Button>
        <Button variant="ghost" onClick={onClose}>{t('common.cancel')}</Button>
        <Button disabled={disabled || !loaded} onClick={() => { onSave(draft.x === 0.5 && draft.y === 0.5 ? undefined : draft); onClose() }}>{t('common.save')}</Button>
      </DialogFooter>
    </DialogContent>
  </Dialog>
}
