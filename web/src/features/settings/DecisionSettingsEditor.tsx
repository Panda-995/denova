import { useId } from 'react'
import { useTranslation } from 'react-i18next'
import { Plus, Trash2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Field, FieldDescription, FieldGroup, FieldLabel, FieldLegend, FieldSet } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { randomUUID } from '@/lib/uuid'
import { ApiKeyInput } from './ApiKeyInput'
import { SettingsDisclosureCard } from './SettingsDisclosureCard'
import { DECISION_ENDPOINT_FIELD_KEYS, DECISION_PROFILE_FIELD_KEYS, type SettingsFieldRequest } from './settings-sections'
import type { DecisionEndpointSettings, DecisionProfileSettings, DecisionSettings } from './types'

export const EMPTY_DECISION_SETTINGS: DecisionSettings = {
  enabled: false, endpoints: [], profiles: [], default_profile_id: '',
  game_presentation: true, lore_classification: true, semantic_triggers: true,
}
const NO_PROFILE = '__none__'

export function DecisionSettingsEditor({ value, onChange, fieldRequest }: {
  value: DecisionSettings
  onChange: (value: DecisionSettings) => void
  fieldRequest?: SettingsFieldRequest
}) {
  const { t } = useTranslation()
  const id = useId()
  const endpoints = value.endpoints ?? []
  const profiles = value.profiles ?? []
  const selectedProfile = profiles.find(profile => profile.id === value.default_profile_id)
  const selectedEndpoint = endpoints.find(endpoint => endpoint.id === selectedProfile?.endpoint_id)
  const change = <K extends keyof DecisionSettings>(key: K, next: DecisionSettings[K]) => onChange({ ...value, [key]: next })
  const updateEndpoint = (endpointID: string, patch: Partial<DecisionEndpointSettings>) => change('endpoints', endpoints.map(endpoint => endpoint.id === endpointID ? { ...endpoint, ...patch } : endpoint))
  const updateProfile = (profileID: string, patch: Partial<DecisionProfileSettings>) => change('profiles', profiles.map(profile => profile.id === profileID ? { ...profile, ...patch } : profile))
  const endpointLabel = (endpoint: DecisionEndpointSettings) => endpoint.name.trim() || t('settings.model.endpointUntitled')
  const profileLabel = (profile: DecisionProfileSettings) => profile.name.trim() || profile.model.trim() || t('settings.decision.profileUntitled')
  const configured = selectedProfile?.enabled !== false && Boolean(selectedProfile?.model.trim()) && selectedEndpoint?.enabled !== false && validEndpoint(selectedEndpoint?.endpoint ?? '')
  const searchProfile = selectedProfile ?? profiles[0]
  const searchEndpointID = endpoints.find(endpoint => endpoint.id === searchProfile?.endpoint_id)?.id ?? endpoints[0]?.id
  const revealProfile = Boolean(fieldRequest?.fieldKey && DECISION_PROFILE_FIELD_KEYS.includes(fieldRequest.fieldKey))
  const revealEndpoint = revealProfile || Boolean(fieldRequest?.fieldKey && DECISION_ENDPOINT_FIELD_KEYS.includes(fieldRequest.fieldKey))

  return (
    <FieldGroup className="gap-4">
      <FieldDescription>{t('settings.decision.description')}</FieldDescription>
      <Field orientation="horizontal">
        <FieldLabel htmlFor={`${id}-enabled`}>{t('settings.decision.enabled')}</FieldLabel>
        <Switch id={`${id}-enabled`} checked={value.enabled} onCheckedChange={next => change('enabled', next)} />
      </Field>
      <Field>
        <FieldLabel htmlFor={`${id}-default`}>{t('settings.decision.defaultProfile')}</FieldLabel>
        <Select value={selectedProfile?.id ?? NO_PROFILE} onValueChange={next => change('default_profile_id', next === NO_PROFILE ? '' : next)}>
          <SelectTrigger id={`${id}-default`} className="w-full"><SelectValue /></SelectTrigger>
          <SelectContent>
            <SelectItem value={NO_PROFILE}>{t('settings.decision.noDefaultProfile')}</SelectItem>
            {profiles.map(profile => {
              const endpoint = endpoints.find(candidate => candidate.id === profile.endpoint_id)
              const disabled = profile.enabled === false || !profile.model.trim() || !endpoint || endpoint.enabled === false || !validEndpoint(endpoint.endpoint)
              const label = `${profileLabel(profile)} · ${endpoint ? endpointLabel(endpoint) : t('settings.model.endpointRouteMissing')}`
              return <SelectItem key={profile.id} value={profile.id} disabled={disabled}>{disabled ? t('settings.model.disabledOption', { name: label }) : label}</SelectItem>
            })}
          </SelectContent>
        </Select>
        {value.enabled && !configured && <FieldDescription>{t('settings.decision.incomplete')}</FieldDescription>}
      </Field>
      <div className="flex flex-col gap-2.5">
        {endpoints.length === 0 && <div className="rounded-md border border-dashed border-[var(--nova-border)] px-3 py-4 text-xs text-[var(--nova-text-faint)]">{t('settings.decision.endpointEmpty')}</div>}
        {endpoints.map((endpoint, endpointIndex) => {
          const endpointProfiles = profiles.filter(profile => profile.endpoint_id === endpoint.id)
          const endpointID = `${id}-${endpoint.id}`
          const endpointValid = !endpoint.endpoint || validEndpoint(endpoint.endpoint)
          return (
            <SettingsDisclosureCard key={endpoint.id} level="connection"
              badge={t('settings.model.endpointName', { index: endpointIndex + 1 })}
              title={endpointLabel(endpoint)}
              subtitle={t('settings.model.endpointSummary', { route: endpoint.endpoint || t('settings.model.endpointRouteMissing'), count: endpointProfiles.length })}
              defaultOpen={!validEndpoint(endpoint.endpoint) || endpointProfiles.length === 0}
              revealRequest={revealEndpoint && endpoint.id === searchEndpointID ? fieldRequest : undefined}
              actions={(
                <div className="flex items-center gap-3 pl-2">
                  <Switch checked={endpoint.enabled !== false} onCheckedChange={enabled => updateEndpoint(endpoint.id, { enabled })} aria-label={t('settings.model.endpointEnabled', { name: endpointLabel(endpoint) })} />
                  <Button type="button" variant="ghost" size="icon-sm" disabled={endpointProfiles.length > 0}
                    title={t(endpointProfiles.length ? 'settings.model.endpointDeleteBlocked' : 'settings.model.deleteEndpoint')}
                    aria-label={t('settings.model.deleteEndpoint')} onClick={() => change('endpoints', endpoints.filter(candidate => candidate.id !== endpoint.id))}><Trash2 /></Button>
                </div>
              )}
            >
              <div className="grid gap-3 p-3 md:grid-cols-2">
                <Field>
                  <FieldLabel htmlFor={`${endpointID}-name`}>{t('settings.model.endpointAliasLabel')}</FieldLabel>
                  <Input id={`${endpointID}-name`} value={endpoint.name} placeholder={t('settings.model.endpointAliasPlaceholder')} onChange={event => updateEndpoint(endpoint.id, { name: event.target.value })} />
                </Field>
                <Field>
                  <FieldLabel>{t('settings.decision.apiKey')}</FieldLabel>
                  <ApiKeyInput label={t('settings.decision.apiKey')} value={endpoint.api_key} placeholder="" onChange={api_key => updateEndpoint(endpoint.id, { api_key })} />
                </Field>
                <Field className="md:col-span-2" data-invalid={!endpointValid}>
                  <FieldLabel htmlFor={`${endpointID}-url`}>{t('settings.decision.endpoint')}</FieldLabel>
                  <Input id={`${endpointID}-url`} aria-invalid={!endpointValid} value={endpoint.endpoint} placeholder="https://api.typesafe.ai/v1/systemone" onChange={event => updateEndpoint(endpoint.id, { endpoint: event.target.value })} />
                  <FieldDescription>{t('settings.decision.endpointHelp')}</FieldDescription>
                  {!endpointValid && <p role="alert" className="text-sm text-destructive">{t('settings.decision.invalidEndpoint')}</p>}
                </Field>
              </div>
              <div className="border-t border-[var(--nova-border)] p-2.5">
                <div className="mb-2 flex flex-wrap items-center justify-between gap-2">
                  <span className="text-[11px] font-medium text-[var(--nova-text-muted)]">{t('settings.model.endpointModels', { count: endpointProfiles.length })}</span>
                  <Button type="button" variant="outline" size="sm" onClick={() => {
                    const profile: DecisionProfileSettings = { id: randomUUID(), name: '', endpoint_id: endpoint.id, model: '' }
                    onChange({ ...value, profiles: [...profiles, profile], default_profile_id: profiles.length === 0 ? profile.id : value.default_profile_id })
                  }}><Plus data-icon="inline-start" />{t('settings.decision.addProfile')}</Button>
                </div>
                {endpointProfiles.length === 0 && <div className="rounded-md border border-dashed border-[var(--nova-border)] px-2.5 py-3 text-xs text-[var(--nova-text-faint)]">{t('settings.model.endpointModelsEmpty')}</div>}
                <div className={endpointProfiles.length ? 'ml-1 flex flex-col gap-1.5 border-l border-[var(--nova-border)] pl-2.5' : undefined}>
                  {endpointProfiles.map((profile, profileIndex) => {
                    const profileID = `${id}-${profile.id}`
                    return (
                      <SettingsDisclosureCard key={profile.id} level="model"
                        badge={profile.id === value.default_profile_id ? t('settings.model.defaultProfileName') : t('settings.decision.profileName', { index: profileIndex + 1 })}
                        title={profileLabel(profile)} subtitle={profile.model || t('settings.model.profileModelMissing')}
                        defaultOpen={!profile.model.trim()}
                        revealRequest={revealProfile && profile.id === searchProfile?.id ? fieldRequest : undefined}
                        actions={(
                          <div className="flex items-center gap-3 pl-2">
                            <Switch checked={profile.enabled !== false} onCheckedChange={enabled => updateProfile(profile.id, { enabled })} aria-label={t('settings.model.profileEnabled', { name: profileLabel(profile) })} />
                            <Button type="button" variant="ghost" size="icon-sm" aria-label={t('settings.decision.deleteProfile')} onClick={() => onChange({
                              ...value, profiles: profiles.filter(candidate => candidate.id !== profile.id),
                              default_profile_id: value.default_profile_id === profile.id ? '' : value.default_profile_id,
                            })}><Trash2 /></Button>
                          </div>
                        )}
                      >
                        <div className="grid gap-3 p-2.5 md:grid-cols-2">
                          <Field>
                            <FieldLabel htmlFor={`${profileID}-model`}>{t('settings.decision.model')}</FieldLabel>
                            <Input id={`${profileID}-model`} value={profile.model} placeholder="jev-latest" onChange={event => updateProfile(profile.id, { model: event.target.value })} />
                          </Field>
                          <Field>
                            <FieldLabel htmlFor={`${profileID}-name`}>{t('settings.model.profileAliasLabel')}</FieldLabel>
                            <Input id={`${profileID}-name`} value={profile.name} placeholder={t('settings.model.profileAliasPlaceholder')} onChange={event => updateProfile(profile.id, { name: event.target.value })} />
                          </Field>
                        </div>
                      </SettingsDisclosureCard>
                    )
                  })}
                </div>
              </div>
            </SettingsDisclosureCard>
          )
        })}
        <Button type="button" variant="outline" size="sm" className="self-start" onClick={() => change('endpoints', [...endpoints, { id: randomUUID(), name: '', endpoint: '', api_key: '' }])}>
          <Plus data-icon="inline-start" />{t('settings.model.addEndpoint')}
        </Button>
      </div>
      <FieldSet>
        <FieldLegend>{t('settings.decision.scenes')}</FieldLegend>
        <FieldGroup className="gap-3">
          {(['game_presentation', 'lore_classification', 'semantic_triggers'] as const).map(scene => (
            <Field key={scene} orientation="horizontal">
              <FieldLabel htmlFor={`${id}-${scene}`}>{t(`settings.decision.${scene}`)}</FieldLabel>
              <Switch id={`${id}-${scene}`} checked={value[scene]} onCheckedChange={next => change(scene, next)} />
            </Field>
          ))}
        </FieldGroup>
      </FieldSet>
      <FieldDescription>{t('settings.decision.fallback')}</FieldDescription>
    </FieldGroup>
  )
}

function validEndpoint(endpoint: string): boolean {
  try {
    const url = new URL(endpoint)
    return ['http:', 'https:'].includes(url.protocol) && !url.username && !url.password
  } catch {
    return false
  }
}
