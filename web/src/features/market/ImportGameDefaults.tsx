import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Checkbox } from '@/components/ui/checkbox'
import { Field, FieldDescription, FieldLabel } from '@/components/ui/field'
import { fetchProjectSettings } from '@/features/settings/api'
import { gameDefaultFields, type GameCreationDefaults, type GameDefaultField } from '@/features/interactive/game-creation-defaults'
import { GameDefaultsSelection, type GameDefaultOption } from '@/features/interactive/components/GameDefaultsSelection'
import type { PackagePreview } from './api'

export function packageDefaultOptions(candidate: PackagePreview, resources: string[]): GameDefaultOption[] {
  const defaults = candidate.game_defaults
  if (!defaults) return []
  return gameDefaultFields.filter(field => defaults[field] !== undefined).map(field => {
    const value = defaults[field]
    const ids = field === 'default_background' ? [defaults.default_background!.resource_id]
      : Array.isArray(value) ? value : [value as string]
    return { field, name: ids.map(id => candidate.resources.find(resource => resource.id === id)?.name || id).join(' · '), available: ids.every(id => resources.includes(id)) }
  })
}

export function ImportGameDefaults({ candidate, resources, projectID, newBook, selected, onChange }: {
  candidate: PackagePreview; resources: string[]; projectID: string; newBook: boolean
  selected: GameDefaultField[]; onChange: (fields: GameDefaultField[]) => void
}) {
  const { t } = useTranslation()
  const [current, setCurrent] = useState<{ projectID: string; defaults?: GameCreationDefaults }>()
  const [failed, setFailed] = useState(false)
  useEffect(() => {
    if (newBook || !projectID) return
    let cancelled = false
    setFailed(false)
    void fetchProjectSettings(projectID).then(settings => {
      if (!cancelled) setCurrent({ projectID, defaults: settings.workspace?.game_creation_defaults })
    }).catch(error => {
      console.error('[game-defaults] failed to load book defaults', error)
      if (!cancelled) setFailed(true)
    })
    return () => { cancelled = true }
  }, [newBook, projectID])
  const ready = newBook || (!!projectID && current?.projectID === projectID)
  const options = packageDefaultOptions(candidate, resources).map(option => ({ ...option,
    previous: !newBook && current?.projectID === projectID && current.defaults?.[option.field] !== undefined ? t('gameDefaults.configured') : undefined,
  }))
  return <section className="space-y-3 rounded-xl border p-4" aria-label={t('gameDefaults.packageTitle')}>
    <Field orientation="horizontal">
      <Checkbox id="adopt-game-defaults" disabled={!ready} checked={selected.length > 0} onCheckedChange={checked => onChange(checked ? options.filter(option => option.available && option.previous === undefined).map(option => option.field) : [])} />
      <FieldLabel htmlFor="adopt-game-defaults">{t('gameDefaults.adopt')}</FieldLabel>
    </Field>
    <FieldDescription>{t('gameDefaults.help')}</FieldDescription>
    {failed && <p role="alert" className="text-sm text-destructive">{t('gameDefaults.loadFailed')}</p>}
    <GameDefaultsSelection options={options} selected={selected} onChange={onChange} disabled={!ready} />
  </section>
}
