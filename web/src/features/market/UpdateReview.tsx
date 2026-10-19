import { useTranslation } from 'react-i18next'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { ButtonGroup } from '@/components/ui/button-group'
import { Field } from '@/components/ui/field'
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import type { PlanRequest, UpdateItem } from './api'

export function UpdateReview({ items, resolutions, busy, onResolve }: {
  items: UpdateItem[]
  resolutions: NonNullable<PlanRequest['resolutions']>
  busy: boolean
  onResolve: (items: UpdateItem[], choice: string) => void
}) {
  const { t } = useTranslation()
  const conflicts = items.filter(item => item.conflict)
  const choices = conflicts.map(item => resolutions[item.resource_id]?.[item.member_id || ''] || '')
  return <section className="flex min-w-0 flex-col gap-3" aria-label={t('market.update.review')}>
    <p className="text-sm text-muted-foreground">{t('market.update.help')}</p>
    {conflicts.length > 0 && <div className="space-y-2">
      <p role="status" className="text-sm text-muted-foreground">{t('market.update.conflictSummary', {
        count: conflicts.length,
        replaceCount: choices.filter(choice => choice === 'remote').length,
        keepCount: choices.filter(choice => choice === 'keep').length,
        pendingCount: choices.filter(choice => choice === '').length,
      })}</p>
      <ButtonGroup aria-label={t('market.update.resolveAll')} className="w-full">
        <Button variant="outline" className="h-auto min-h-8 flex-1 whitespace-normal" disabled={busy} onClick={() => onResolve(conflicts, 'remote')}>{t('market.update.replaceAll')}</Button>
        <Button variant="outline" className="h-auto min-h-8 flex-1 whitespace-normal" disabled={busy} onClick={() => onResolve(conflicts, 'keep')}>{t('market.update.keepAll')}</Button>
        <Button variant="outline" className="h-auto min-h-8 flex-1 whitespace-normal" disabled={busy} onClick={() => onResolve(conflicts, '')}>{t('market.update.pendingAll')}</Button>
      </ButtonGroup>
    </div>}
    <ul className="divide-y rounded-lg border">
      {items.map((item) => {
        const resolution = resolutions[item.resource_id]?.[item.member_id || ''] || ''
        const state = item.conflict ? resolution === 'remote' ? 'update' : resolution === 'keep' ? 'keep' : 'conflict' : item.state
        return <li key={JSON.stringify([item.resource_id, item.member_id])} className="flex min-w-0 flex-col gap-2 p-3">
          <div className="flex min-w-0 flex-wrap items-center justify-between gap-2">
            <span className="min-w-0 break-words [overflow-wrap:anywhere]">{item.name}</span>
            <Badge variant="outline">{t(`market.update.states.${state}`)}</Badge>
          </div>
          {item.conflict && <Field>
            <Select value={resolution || 'pending'} disabled={busy} onValueChange={value => onResolve([item], value === 'pending' ? '' : value)}>
              <SelectTrigger aria-label={t('market.update.resolve', { name: item.name })} id={`resolution-${item.resource_id}-${item.member_id || ''}`} className="w-full"><SelectValue /></SelectTrigger>
              <SelectContent><SelectGroup>
                <SelectItem value="pending">{t('market.update.pending')}</SelectItem>
                <SelectItem value="keep">{t('market.update.keep')}</SelectItem>
                <SelectItem value="remote">{t('market.update.remote')}</SelectItem>
              </SelectGroup></SelectContent>
            </Select>
          </Field>}
        </li>
      })}
    </ul>
  </section>
}
