import { modelProfileID, modelProfileLabel, modelProfilesWithDefault, selectableModelProfiles } from './model-profiles'
import type { LayeredSettings, ModelProfileSettings } from './types'

export interface ModelProfileOption {
  id: string
  label: string
  modelLabel: string
}

export function buildModelProfileOptions(
  settings: LayeredSettings | null,
  t: (key: string, options?: Record<string, unknown>) => string,
): ModelProfileOption[] {
  if (!settings) return []
  const profiles = new Map<string, string>()
  const add = (profile?: ModelProfileSettings) => {
    const id = modelProfileID(profile)
    if (!id) return
    profiles.set(id, modelProfileLabel(profile))
  }
  selectableModelProfiles(settings.effective).forEach(add)
  if (modelProfilesWithDefault(settings.effective).length === 0) profiles.set('default', t('chat.modelProfile.defaultModel'))
  return Array.from(profiles.entries()).map(([id, label]) => ({
    id,
    modelLabel: label,
    label: id === 'default'
      ? t('chat.modelProfile.defaultProfile', { label })
      : t('chat.modelProfile.profile', { id, label }),
  }))
}
