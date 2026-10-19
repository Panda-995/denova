import { useId } from 'react'
import { useTranslation } from 'react-i18next'
import { ChevronDown } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Field, FieldLabel } from '@/components/ui/field'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { InlineErrorNotice } from '@/components/common/inline-error-notice'
import type { LoreIndexMembership } from '@/lib/api-client/types'
import { useLoreIndex } from './use-lore-index'

export function LoreIndexDetailToggle({ value, onChange, inheritLabel, label, disabled }: {
  value: LoreIndexMembership['detail']
  onChange: (detail: LoreIndexMembership['detail']) => void
  inheritLabel?: string
  label: string
  disabled?: boolean
}) {
  const { t } = useTranslation()
  return <ToggleGroup type="single" variant="outline" spacing={1} value={value} disabled={disabled} aria-label={label} className="max-w-full flex-wrap"
    onValueChange={detail => { if (detail) onChange(detail as LoreIndexMembership['detail']) }}>
    {inheritLabel && <ToggleGroupItem value="inherit">{t('lore.index.inherit', { detail: inheritLabel })}</ToggleGroupItem>}
    {(['name', 'brief', 'full'] as const).map(detail => <ToggleGroupItem key={detail} value={detail}>{t(`lore.index.detail.${detail}`)}</ToggleGroupItem>)}
  </ToggleGroup>
}

/** Compact multi-group field. Detail overrides stay inside the picker so the
 * editor's metadata layout does not grow with the number of memberships. */
export function LoreIndexMemberships({ projectId, memberships = [], onChange }: {
  projectId: string
  memberships?: LoreIndexMembership[]
  onChange: (memberships: LoreIndexMembership[]) => void
}) {
  const { t } = useTranslation()
  const id = useId()
  const index = useLoreIndex(projectId)
  const groups = index.data?.guide.groups ?? []
  const names = memberships.map(m => groups.find(group => group.id === m.group_id)?.name ?? m.group_id)
  return <Field className="min-w-0 gap-1.5" data-testid="lore-index-memberships">
    <FieldLabel htmlFor={id} className="text-[11px] font-normal text-muted-foreground">{t('lore.index.memberships')}</FieldLabel>
    <Popover>
      <PopoverTrigger asChild>
        <Button id={id} type="button" variant="outline" className="h-8 w-full min-w-0 justify-between font-normal"
          aria-label={t('lore.index.memberships')} title={names.join(', ')}>
          <span className="min-w-0 truncate">{names.length ? names.join(', ') : t('lore.index.automatic')}</span>
          <ChevronDown data-icon="inline-end" />
        </Button>
      </PopoverTrigger>
      <PopoverContent align="start" className="max-h-[var(--radix-popover-content-available-height)] w-80 overflow-y-auto" aria-label={t('lore.index.memberships')}>
        {index.isError ? <InlineErrorNotice message={t('lore.index.loadFailed')} />
          : index.isPending ? <p className="text-muted-foreground">{t('common.loading')}</p>
          : !groups.length ? <p className="text-muted-foreground">{t('lore.index.noCustomGroups')}</p>
          : groups.map(group => {
            const membership = memberships.find(m => m.group_id === group.id)
            return <div key={group.id} className="flex min-w-0 items-center gap-2">
              <FieldLabel className="min-w-0 flex-1 cursor-pointer py-2">
                <Checkbox checked={Boolean(membership)} onCheckedChange={checked => onChange(checked
                  ? [...memberships, { group_id: group.id, detail: 'inherit' }]
                  : memberships.filter(m => m.group_id !== group.id))} />
                <span className="min-w-0 break-all">{group.name}</span>
              </FieldLabel>
              {membership && <Select value={membership.detail}
                onValueChange={detail => onChange(memberships.map(m => m.group_id === group.id ? { ...m, detail: detail as LoreIndexMembership['detail'] } : m))}>
                <SelectTrigger size="sm" aria-label={t('lore.index.itemDetail', { name: group.name })}><SelectValue /></SelectTrigger>
                <SelectContent><SelectGroup>
                  <SelectItem value="inherit">{t('lore.index.inherit', { detail: t(`lore.index.detail.${group.default_detail}`) })}</SelectItem>
                  {(['name', 'brief', 'full'] as const).map(detail => <SelectItem key={detail} value={detail}>{t(`lore.index.detail.${detail}`)}</SelectItem>)}
                </SelectGroup></SelectContent>
              </Select>}
            </div>
          })}
      </PopoverContent>
    </Popover>
  </Field>
}
