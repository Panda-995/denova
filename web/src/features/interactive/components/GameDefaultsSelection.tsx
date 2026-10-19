import { useId } from 'react'
import { useTranslation } from 'react-i18next'
import { Checkbox } from '@/components/ui/checkbox'
import { Field, FieldDescription, FieldLabel } from '@/components/ui/field'
import type { GameDefaultField } from '../game-creation-defaults'

export interface GameDefaultOption {
  field: GameDefaultField
  name: string
  available: boolean
  previous?: string
}

export function GameDefaultsSelection({ options, selected, onChange, disabled = false }: {
  options: GameDefaultOption[]
  selected: GameDefaultField[]
  onChange: (fields: GameDefaultField[]) => void
  disabled?: boolean
}) {
  const { t } = useTranslation()
  const id = useId()
  return <div className="space-y-3">
    {options.map(option => <Field key={option.field} orientation="horizontal" className="items-start rounded-lg border p-3">
      <Checkbox id={`${id}-${option.field}`} disabled={disabled || !option.available} checked={selected.includes(option.field) && option.available}
        onCheckedChange={checked => onChange(checked ? [...selected, option.field] : selected.filter(field => field !== option.field))} />
      <div className="min-w-0 flex-1 space-y-1 [overflow-wrap:anywhere]">
        <FieldLabel htmlFor={`${id}-${option.field}`} className="block w-full">{t(`gameDefaults.fields.${option.field}`)} · {option.name}</FieldLabel>
        {!option.available ? <FieldDescription>{t('gameDefaults.unavailable')}</FieldDescription>
          : option.previous !== undefined ? <FieldDescription>{t(selected.includes(option.field) ? 'gameDefaults.replacing' : 'gameDefaults.keeping', { name: option.previous })}</FieldDescription> : null}
      </div>
    </Field>)}
  </div>
}
