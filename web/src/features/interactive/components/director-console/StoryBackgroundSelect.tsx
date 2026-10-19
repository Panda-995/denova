import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Crosshair } from 'lucide-react'
import { BackgroundFocusDialog } from './BackgroundFocusDialog'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { getProjectLoreItems, type LoreItem } from '@/lib/api'
import { projectFileAssetURL } from '@/lib/api-client/project-files'
import { MaterialImage } from '@/features/lore/MaterialImage'
import type { PresentationMaterial } from '../../types'
import { TuningRow } from './StoryTuningControls'

export function StoryBackgroundSelect({ projectId, value, disabled, onChange }: {
  projectId?: string
  value?: PresentationMaterial
  disabled: boolean
  onChange: (value?: PresentationMaterial) => void
}) {
  const { t } = useTranslation()
  const title = t('storyStage.presentation.currentBackground')
  const [open, setOpen] = useState(false)
  const [focusOpen, setFocusOpen] = useState(false)
  const [search, setSearch] = useState('')
  const [items, setItems] = useState<LoreItem[]>([])
  const [loading, setLoading] = useState(false)
  const [failed, setFailed] = useState(false)
  const [reload, setReload] = useState(0)
  useEffect(() => {
    if (!open || !projectId) return
    let cancelled = false
    setLoading(true)
    setFailed(false)
    void getProjectLoreItems(projectId).then(items => {
      if (!cancelled) setItems(items)
    }).catch(error => {
      console.warn('[story-presentation] failed to load background materials', { projectId, error })
      if (!cancelled) setFailed(true)
    }).finally(() => { if (!cancelled) setLoading(false) })
    return () => { cancelled = true }
  }, [open, projectId, reload])

  const options = items.filter(item => item.enabled).flatMap(item =>
    (item.resolved_materials ?? []).filter(asset => asset.path && asset.mime_type.startsWith('image/')).map(asset => ({
      itemName: item.name,
      material: { item_id: item.id, asset_id: asset.id, path: asset.path!, name: asset.name },
    })),
  )
  const filtered = options.filter(({ itemName, material }) => `${itemName} ${material.name}`.toLocaleLowerCase().includes(search.trim().toLocaleLowerCase()))
  const choose = (material?: PresentationMaterial) => {
    onChange(material)
    setOpen(false)
  }
  return (
    <>
      <TuningRow title={title}>
        <Button variant="outline" size="sm" disabled={disabled || !projectId} onClick={() => { setSearch(''); setOpen(true) }} aria-label={title} title={value?.name} className="max-w-40 min-w-0">
          <span className="truncate">{value?.name || t('storyStage.presentation.noBackground')}</span>
        </Button>
      </TuningRow>
      {value?.path && projectId && <div className="relative mx-2.5 mb-2.5">
        <MaterialImage key={value.path} src={projectFileAssetURL(projectId, value.path)} alt={value.name} focus={value.focus} className="aspect-[3/1] max-h-36 w-full rounded-md object-cover object-center" />
        <Button type="button" variant="secondary" size="icon-sm" disabled={disabled} className="absolute right-1 bottom-1" title={t('storyStage.presentation.focus')} aria-label={t('storyStage.presentation.focus')} onClick={() => setFocusOpen(true)}><Crosshair /></Button>
      </div>}
      {focusOpen && value && projectId && <BackgroundFocusDialog key={`${value.item_id}:${value.asset_id}`} src={projectFileAssetURL(projectId, value.path)} focus={value.focus} disabled={disabled} onClose={() => setFocusOpen(false)} onSave={focus => onChange({ ...value, focus })} />}
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="flex max-h-[85dvh] flex-col sm:max-w-2xl">
          <DialogHeader>
            <DialogTitle>{title}</DialogTitle>
            <DialogDescription>{t('storyStage.presentation.backgroundPickerHelp')}</DialogDescription>
          </DialogHeader>
          <Input value={search} onChange={event => setSearch(event.target.value)} placeholder={t('storyStage.presentation.searchBackgrounds')} aria-label={t('storyStage.presentation.searchBackgrounds')} />
          <div className="min-h-0 overflow-y-auto">
            <Button variant="outline" disabled={disabled} onClick={() => choose()}>{t('storyStage.presentation.noBackground')}</Button>
            {loading ? <p role="status" className="py-6 text-sm text-muted-foreground">{t('common.loading')}</p> : failed ? <div className="py-6 text-sm text-muted-foreground" role="alert">
              <p>{t('storyStage.presentation.loadBackgroundsFailed')}</p>
              <Button variant="ghost" onClick={() => setReload(reload + 1)}>{t('common.retry')}</Button>
            </div> : <div className="mt-3 grid grid-cols-2 gap-3 sm:grid-cols-3">
              {filtered.map(({ itemName, material }) => <button key={`${material.item_id}:${material.asset_id}`} type="button" disabled={disabled} aria-pressed={value?.item_id === material.item_id && value.asset_id === material.asset_id} onClick={() => choose(material)} className="min-w-0 overflow-hidden rounded-lg border border-border text-left transition-colors hover:border-ring focus-visible:outline-2 focus-visible:outline-ring aria-pressed:border-primary">
                <MaterialImage src={projectFileAssetURL(projectId!, material.path)} alt="" className="aspect-video w-full object-cover" />
                <span className="block truncate px-2 pt-2 text-sm" title={material.name}>{material.name}</span>
                <span className="block truncate px-2 pb-2 text-xs text-muted-foreground" title={itemName}>{itemName}</span>
              </button>)}
            </div>}
            {!loading && !failed && !filtered.length && <p className="py-6 text-sm text-muted-foreground">{t('storyStage.presentation.noBackgrounds')}</p>}
          </div>
        </DialogContent>
      </Dialog>
    </>
  )
}
