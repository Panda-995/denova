import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { getBooks, getProjectLoreItems, type BookRecord, type LoreItem } from '@/lib/api'
import { refreshProjectSettings, patchProjectSettings } from '@/features/settings/api'
import type { LayeredSettings } from '@/features/settings/types'
import { getActorStates, getEventPackages, getGamePlanningTemplates, getImagePresets, getInteractiveTellers, getRuleSystems } from '../api'
import { gamePlanningTemplateName } from '../game-planning'
import { narrativeStyleName } from '../narrative-style'
import { gameDefaultFields, selectedGameDefaults, type GameCreationDefaults, type GameDefaultField } from '../game-creation-defaults'
import { GameDefaultsSelection, type GameDefaultOption } from './GameDefaultsSelection'

// Shared by acquired packages and the new-Story form. Saving copies only the
// reviewed resource choices; it never subscribes the Project to a package.
export function GameDefaultsDialog({ defaults, projectID: initialProjectID = '', onClose }: {
  defaults: GameCreationDefaults; projectID?: string; onClose: () => void
}) {
  const { t } = useTranslation()
  const [projectID, setProjectID] = useState(initialProjectID)
  const [books, setBooks] = useState<BookRecord[]>([])
  const [loaded, setLoaded] = useState<{ projectID: string; settings: LayeredSettings; options: GameDefaultOption[] }>()
  const [selected, setSelected] = useState<GameDefaultField[]>([])
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [reload, setReload] = useState(0)
  useEffect(() => {
    if (initialProjectID) return
    let alive = true
    void getBooks().then(books => { if (alive) setBooks(books) }).catch(error => {
      console.error('[game-defaults] failed to load books', error)
      if (alive) setError(t('gameDefaults.loadFailed'))
    })
    return () => { alive = false }
  }, [initialProjectID, t])
  useEffect(() => {
    if (!projectID) return
    let alive = true
    setLoaded(undefined)
    setError('')
    void Promise.all([refreshProjectSettings(projectID), getProjectLoreItems(projectID), getInteractiveTellers(), getImagePresets(), getGamePlanningTemplates(), getActorStates(), getRuleSystems(), getEventPackages()])
      .then(([settings, lore, narrative, images, planning, actors, rules, events]) => {
        if (!alive) return
        const catalogs: Partial<Record<GameDefaultField, { id: string; name: string; invalid?: boolean }[]>> = {
          narrative_style_id: narrative.map(item => ({ ...item, name: narrativeStyleName(item, t) })), image_preset_id: images,
          planning_template_id: planning.map(item => ({ ...item, name: gamePlanningTemplateName(item, t) })),
          actor_state_id: actors, rule_system_id: rules, event_package_ids: events,
        }
        const describe = (values: GameCreationDefaults, field: GameDefaultField) => {
          const value = values[field]
          if (field === 'default_background') {
            const bg = values.default_background
            if (bg?.mode === 'none') return { name: t('gameDefaults.none'), available: true }
            const item: LoreItem | undefined = lore.find(item => item.id === bg?.item_id && item.enabled)
            const asset = item?.resolved_materials?.find(asset => asset.id === bg?.asset_id && asset.path && asset.mime_type.startsWith('image/'))
            return { name: asset ? `${item!.name} · ${asset.name}` : t('gameDefaults.unavailable'), available: !!asset }
          }
          const ids = Array.isArray(value) ? value : value === '' ? [] : [value as string]
          const resources = ids.map(id => catalogs[field]?.find(item => item.id === id && !item.invalid))
          return { name: ids.length ? resources.map(item => item?.name || t('gameDefaults.unavailable')).join(' · ') : t('gameDefaults.none'), available: resources.every(Boolean) }
        }
        const current = settings.workspace?.game_creation_defaults
        const options = gameDefaultFields.filter(field => defaults[field] !== undefined).map(field => ({
          field, ...describe(defaults, field), previous: current?.[field] !== undefined ? describe(current, field).name : undefined,
        }))
        setLoaded({ projectID, settings, options })
        setSelected(options.filter(option => option.available && option.previous === undefined).map(option => option.field))
      }).catch(error => {
        console.error('[game-defaults] failed to load resource choices', error)
        if (alive) setError(t('gameDefaults.loadFailed'))
      })
    return () => { alive = false }
  }, [defaults, projectID, reload, t])
  const save = async (clear = false) => {
    if (loaded?.projectID !== projectID) return
    setBusy(true)
    setError('')
    try {
      await patchProjectSettings(projectID, 'workspace', { game_creation_defaults: clear ? null : selectedGameDefaults(defaults, selected) }, loaded.settings.revisions?.workspace)
      toast.success(t('gameDefaults.saved'))
      onClose()
    } catch (error) {
      console.error('[game-defaults] failed to save book defaults', error)
      setError(error instanceof Error ? error.message : t('gameDefaults.saveFailed'))
    } finally { setBusy(false) }
  }
  return <Dialog open onOpenChange={open => { if (!open && !busy) onClose() }}>
    <DialogContent className="max-h-[90dvh] overflow-y-auto">
      <DialogHeader><DialogTitle>{t('gameDefaults.title')}</DialogTitle><DialogDescription>{t('gameDefaults.help')}</DialogDescription></DialogHeader>
      {!initialProjectID && <Select value={projectID} onValueChange={setProjectID} disabled={busy}>
        <SelectTrigger aria-label={t('market.import.project')}><SelectValue placeholder={t('market.import.selectProject')} /></SelectTrigger>
        <SelectContent>{books.filter(book => book.project_id).map(book => <SelectItem key={book.project_id} value={book.project_id!}>{book.name}</SelectItem>)}</SelectContent>
      </Select>}
      {loaded?.projectID === projectID && <GameDefaultsSelection options={loaded.options} selected={selected} onChange={setSelected} disabled={busy} />}
      {!loaded && projectID && !error && <p role="status" className="text-sm text-muted-foreground">{t('common.loading')}</p>}
      {error && <div role="alert" className="space-y-2 text-sm text-destructive"><p>{error}</p><Button variant="outline" onClick={() => setReload(value => value + 1)} disabled={busy}>{t('common.retry')}</Button></div>}
      <DialogFooter className="flex-wrap">
        {loaded?.settings.workspace?.game_creation_defaults && <Button variant="ghost" disabled={busy} onClick={() => void save(true)}>{t('gameDefaults.clear')}</Button>}
        <Button variant="outline" disabled={busy} onClick={onClose}>{t('common.cancel')}</Button>
        <Button disabled={busy || loaded?.projectID !== projectID || !selected.length} onClick={() => void save()}>{t('common.save')}</Button>
      </DialogFooter>
    </DialogContent>
  </Dialog>
}
