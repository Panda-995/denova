import { useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Check, ImagePlus, Sparkles, Upload } from 'lucide-react'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import { Spinner } from '@/components/ui/spinner'
import {
  createAgentCommandID,
  generateLoreItemImage,
  loreImageURL,
  loreMaterialURL,
  mutateLoreMaterial,
  uploadLoreItemMaterial,
  type LoreItem,
  type LoreItemImageGenerateRequest,
} from '@/lib/api'
import { notifyLoreUpdated } from './events'
import { MaterialImage } from './MaterialImage'
import { LoreMaterialGenerateDialog } from './LoreMaterialGenerateDialog'
import type { LoreCoverAction } from './LoreCard'
import { cn } from '@/lib/utils'

/** Cover actions append media first; changing the cover never removes old images. */
export function LoreCoverDialog({
  projectId,
  item,
  action,
  imageConfigured,
  onChanged,
  onClose,
}: {
  projectId: string
  item: LoreItem
  action: LoreCoverAction
  imageConfigured: boolean
  onChanged: (item: LoreItem) => void
  onClose: () => void
}) {
  const { t } = useTranslation()
  const [generateOpen, setGenerateOpen] = useState(action === 'generate')
  const [busy, setBusy] = useState(false)
  const running = useRef(false)
  const [selectedID, setSelectedID] = useState('')
  const fileInput = useRef<HTMLInputElement>(null)
  const images = (item.resolved_materials ?? []).filter((material) =>
    material.mime_type.startsWith('image/'),
  )
  const selected = images.find((material) => material.id === selectedID)
  const accept = (saved: LoreItem) => {
    onChanged(saved)
    notifyLoreUpdated({ projectId, ids: [saved.id], source: 'materials' })
  }
  const perform = async (operation: () => Promise<void>) => {
    if (running.current) return false
    running.current = true
    setBusy(true)
    try {
      await operation()
      return true
    } catch (error) {
      console.error('[lore-cover] operation failed', { projectId, itemId: item.id, error })
      toast.error(error instanceof Error ? error.message : t('lore.materials.failed'))
      return false
    } finally {
      running.current = false
      setBusy(false)
    }
  }
  const generate = (request: LoreItemImageGenerateRequest) =>
    perform(async () => {
      const previousLocations = new Set(images.map((image) => loreMaterialURL(projectId, image)))
      const saved = await generateLoreItemImage(projectId, item.id, {
        ...request,
        command_id: request.mode === 'agent' ? createAgentCommandID() : undefined,
      })
      accept(saved)
      const generated = saved.resolved_materials?.find(
        (image) =>
          image.mime_type.startsWith('image/') &&
          !previousLocations.has(loreMaterialURL(projectId, image)),
      )
      if (generated) {
        setSelectedID(generated.id)
        // The user requested a cover, but another editor may have set one meanwhile.
        if (!loreImageURL(projectId, item))
          accept(
            await mutateLoreMaterial(projectId, item.id, {
              op: 'cover_if_missing',
              asset_id: generated.id,
            }),
          )
      }
      setGenerateOpen(false)
      toast.success(t('lore.library.generated'))
    })
  const upload = (file: File) =>
    perform(async () => {
      const saved = await uploadLoreItemMaterial(projectId, item.id, file)
      accept(saved)
      const uploaded = saved.resolved_materials?.at(-1)
      if (uploaded) {
        setSelectedID(uploaded.id)
        accept(await mutateLoreMaterial(projectId, item.id, { op: 'cover', asset_id: uploaded.id }))
      }
      toast.success(t('lore.library.coverSaved'))
    })

  if (generateOpen)
    return (
      <LoreMaterialGenerateDialog
        busy={busy}
        title={t(
          loreImageURL(projectId, item) ? 'lore.library.regenerate' : 'lore.library.generateCover',
        )}
        description={t('lore.library.generationHint', { name: item.name })}
        onClose={() => setGenerateOpen(false)}
        onGenerate={generate}
      />
    )

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !busy) onClose()
      }}
    >
      <DialogContent className="max-h-[90dvh] overflow-y-auto sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{t('lore.library.coverFor', { name: item.name })}</DialogTitle>
          <DialogDescription>{t('lore.library.coverHint')}</DialogDescription>
        </DialogHeader>
        <div className="flex flex-wrap gap-2">
          <Button
            variant={action === 'upload' ? 'default' : 'outline'}
            disabled={busy}
            onClick={() => fileInput.current?.click()}
          >
            <Upload data-icon="inline-start" />
            {t('lore.library.uploadCover')}
          </Button>
          <Button
            variant="outline"
            disabled={busy || !imageConfigured || !item.enabled}
            onClick={() => setGenerateOpen(true)}
          >
            <Sparkles data-icon="inline-start" />
            {t(
              loreImageURL(projectId, item)
                ? 'lore.library.regenerate'
                : 'lore.library.generateCover',
            )}
          </Button>
          <input
            ref={fileInput}
            type="file"
            accept="image/png,image/jpeg,image/webp,image/gif"
            className="hidden"
            aria-label={t('lore.library.uploadCover')}
            disabled={busy}
            onChange={(event) => {
              const file = event.target.files?.[0]
              event.target.value = ''
              if (file) void upload(file)
            }}
          />
        </div>
        {selected && (
          <div className="flex max-h-64 justify-center overflow-hidden rounded-lg bg-muted">
            <MaterialImage
              key={loreMaterialURL(projectId, selected)}
              src={loreMaterialURL(projectId, selected)}
              alt={selected.name}
              className="max-h-64 max-w-full object-contain"
            />
          </div>
        )}
        {images.length ? (
          <div className="grid grid-cols-[repeat(auto-fill,minmax(min(100%,140px),1fr))] gap-3">
            {images.map((image) => {
              const src = loreMaterialURL(projectId, image)
              const cover = loreImageURL(projectId, item) === src
              return (
                <button
                  type="button"
                  key={image.id}
                  disabled={busy}
                  aria-label={t('lore.library.selectImage', { name: image.name })}
                  aria-pressed={selectedID === image.id}
                  onClick={() => setSelectedID(image.id)}
                  className={cn(
                    'flex min-w-0 flex-col gap-2 overflow-hidden rounded-lg border p-2 text-left focus-visible:ring-2 focus-visible:ring-ring',
                    selectedID === image.id && 'ring-2 ring-ring',
                  )}
                >
                  <div className="aspect-square overflow-hidden rounded bg-muted">
                    <MaterialImage key={src} src={src} alt="" className="size-full object-cover" />
                  </div>
                  <span className="flex min-w-0 items-center gap-1 text-xs">
                    {cover && <Check className="size-3.5 shrink-0" />}
                    <span className="truncate">{image.name}</span>
                  </span>
                </button>
              )
            })}
          </div>
        ) : (
          <Empty>
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <ImagePlus />
              </EmptyMedia>
              <EmptyTitle>{t('lore.library.noCover')}</EmptyTitle>
              <EmptyDescription>{t('lore.library.coverEmpty')}</EmptyDescription>
            </EmptyHeader>
          </Empty>
        )}
        <DialogFooter>
          <Button variant="outline" disabled={busy} onClick={onClose}>
            {t('common.close')}
          </Button>
          <Button
            disabled={busy || !selected}
            onClick={() =>
              void perform(async () => {
                accept(
                  await mutateLoreMaterial(projectId, item.id, {
                    op: 'cover',
                    asset_id: selected!.id,
                  }),
                )
                toast.success(t('lore.library.coverSaved'))
                onClose()
              })
            }
          >
            {busy && <Spinner data-icon="inline-start" />}
            {t('lore.materials.setCover')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
