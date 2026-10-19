import { useId } from 'react'
import { useTranslation } from 'react-i18next'
import { SettingsFieldRow } from '@/components/forms/settings-field-row'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import type { Settings } from './types'

export function MarketSettings({ value, defaultURL, inheritedURL, onChange }: {
  value: Settings['market']
  defaultURL: string
  inheritedURL: string
  onChange: (value: Settings['market']) => void
}) {
  const { t } = useTranslation()
  const id = useId()
  return <>
    <SettingsFieldRow title={t('settings.market.registryURL')} description={t('settings.market.hint')} htmlFor={id} controlClassName="flex-1">
      <div className="flex min-w-0 flex-1 flex-col gap-2">
        <Input id={id} type="url" value={value?.registry_url || ''} placeholder={inheritedURL}
          onChange={(event) => onChange({ ...value, registry_url: event.target.value })} />
        <Button className="self-start" size="sm" variant="outline"
          disabled={(value?.registry_url || inheritedURL) === defaultURL}
          onClick={() => onChange({ ...value, registry_url: defaultURL })}>
          {t('settings.market.restoreDefault')}
        </Button>
      </div>
    </SettingsFieldRow>
    <SettingsFieldRow title={t('settings.market.githubToken')} description={t('settings.market.githubTokenHint')} htmlFor={`${id}-token`} controlClassName="flex-1">
      <Input id={`${id}-token`} type="password" autoComplete="off" value={value?.github_token || ''}
        placeholder={t('settings.market.githubTokenPlaceholder')}
        onChange={(event) => onChange({ ...value, github_token: event.target.value })} />
    </SettingsFieldRow>
  </>
}
