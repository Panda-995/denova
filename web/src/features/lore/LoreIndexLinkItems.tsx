import { useState } from 'react'
import { Plus, Search } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Field, FieldGroup, FieldLabel } from '@/components/ui/field'
import { InputGroup, InputGroupAddon, InputGroupInput } from '@/components/ui/input-group'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { Separator } from '@/components/ui/separator'
import type { LoreItem } from '@/lib/api-client/types'

/** Search only filters visibility; selection survives searches. onLink returns
 * saved IDs so a partial failure leaves only unsaved entries selected. */
export function LoreIndexLinkItems({ items, busy, onLink }: {
  items: LoreItem[]
  busy: boolean
  onLink: (items: LoreItem[]) => Promise<string[]>
}) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [search, setSearch] = useState('')
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const selectedItems = items.filter(item => selected.has(item.id))
  const matches = items.filter(item => [item.name, ...(item.keywords ?? []), ...(item.tags ?? [])].some(value => value.toLowerCase().includes(search.trim().toLowerCase())))
  const changeOpen = (value: boolean) => {
    if (busy) return
    setOpen(value)
    setSearch('')
    setSelected(new Set())
  }
  const link = async () => {
    const saved = new Set(await onLink(selectedItems))
    const remaining = new Set(selectedItems.filter(item => !saved.has(item.id)).map(item => item.id))
    setSelected(remaining)
    if (!remaining.size) setOpen(false)
  }
  return <Popover open={open} onOpenChange={changeOpen}>
    <PopoverTrigger asChild><Button size="sm" variant="outline" disabled={busy || !items.length}><Plus data-icon="inline-start" />{t('lore.index.linkItem')}</Button></PopoverTrigger>
    <PopoverContent align="start" className="max-h-[var(--radix-popover-content-available-height)] w-80 max-w-[calc(100vw-2rem)] gap-0 p-0" aria-label={t('lore.index.linkItem')}>
      <div className="shrink-0 p-2">
        <InputGroup>
          <InputGroupInput value={search} onChange={event => setSearch(event.target.value)} placeholder={t('lore.index.searchItems')} aria-label={t('lore.index.searchItems')} disabled={busy} />
          <InputGroupAddon><Search /></InputGroupAddon>
        </InputGroup>
      </div>
      <div className="min-h-0 max-h-72 overflow-y-auto p-1">
        <FieldGroup className="gap-0">
          {matches.map(item => <Field key={item.id} orientation="horizontal">
            <FieldLabel className="flex min-w-0 flex-1 cursor-pointer gap-2 rounded-sm p-2 hover:bg-muted">
              <Checkbox checked={selected.has(item.id)} disabled={busy} onCheckedChange={checked => setSelected(current => {
                const next = new Set(current)
                if (checked) next.add(item.id)
                else next.delete(item.id)
                return next
              })} />
              <span className="min-w-0 flex-1 break-words">{item.name}</span>
              {!item.enabled && <Badge variant="outline">{t('lore.references.disabled')}</Badge>}
            </FieldLabel>
          </Field>)}
        </FieldGroup>
        {!matches.length && <p className="py-6 text-center text-sm text-muted-foreground">{t('lore.references.noMatches')}</p>}
      </div>
      <Separator />
      <div className="flex shrink-0 flex-wrap items-center justify-between gap-2 p-3">
        <span className="text-xs text-muted-foreground" aria-live="polite">{t('lore.index.selectedCount', { count: selectedItems.length })}</span>
        <div className="flex gap-2">
          <Button size="sm" variant="ghost" disabled={busy} onClick={() => changeOpen(false)}>{t('common.cancel')}</Button>
          <Button size="sm" disabled={busy || !selectedItems.length} onClick={() => void link()}>{t('lore.index.linkSelected')}</Button>
        </div>
      </div>
    </PopoverContent>
  </Popover>
}
