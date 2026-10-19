import { useState } from 'react'
import { ArrowDown, ArrowUp, Check, FolderCog, Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useQueryClient } from '@tanstack/react-query'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, DialogTrigger } from '@/components/ui/dialog'
import { Field, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { InlineErrorNotice } from '@/components/common/inline-error-notice'
import { mutateLoreCategory, type LoreCategoryMutation } from '@/lib/api-client/lore'
import { notifyLoreUpdated } from './events'
import { loreCategoriesKey, useLoreCategories } from './use-lore-categories'

/** Project category edits share one dialog across the writing and game libraries. */
export function LoreCategoryManager({ projectId }: { projectId: string }) {
  const { t } = useTranslation()
  const client = useQueryClient()
  const { categories, isPending, isError } = useLoreCategories(projectId)
  const [open, setOpen] = useState(false)
  const [saving, setSaving] = useState(false)
  const busy = saving || isPending || isError
  const [error, setError] = useState('')
  const [name, setName] = useState('')
  const [drafts, setDrafts] = useState<Record<string, string>>({})
  const [removing, setRemoving] = useState('')
  const [destination, setDestination] = useState('')
  const label = (id: string) => categories.find((c) => c.id === id)?.name || t(`lore.type.${id}`)
  const mutate = async (input: LoreCategoryMutation) => {
    setSaving(true)
    setError('')
    try {
      const saved = await mutateLoreCategory(projectId, input)
      client.setQueryData(loreCategoriesKey(projectId), saved)
      notifyLoreUpdated({ projectId, source: 'categories' })
      setName('')
      setDrafts({})
      setRemoving('')
    } catch (cause) {
      console.error('[lore-categories] mutation failed', { projectId, operation: input.op, cause })
      setError(t('lore.categories.invalid'))
    } finally {
      setSaving(false)
    }
  }
  return (
    <Dialog open={open} onOpenChange={(next) => { if (!saving) { setOpen(next); setError(''); setDrafts({}); setRemoving('') } }}>
      <DialogTrigger asChild>
        <Button variant="outline" size="sm"><FolderCog data-icon="inline-start" />{t('lore.categories.manage')}</Button>
      </DialogTrigger>
      <DialogContent className="max-h-[85vh] overflow-y-auto sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>{t('lore.categories.manage')}</DialogTitle>
          <DialogDescription>{t('lore.categories.description')}</DialogDescription>
        </DialogHeader>
        {(error || isError) && <InlineErrorNotice message={error || t('lore.categories.loadFailed')} />}
        <div className="flex min-w-0 flex-col gap-3">
          {categories.map((category, index) => {
            const current = label(category.id)
            const draft = drafts[category.id] ?? current
            const destinations = categories.filter((c) => c.id !== category.id && c.id !== 'character')
            return (
              <div key={category.id} className="flex min-w-0 flex-col gap-2 rounded-md border p-2">
                <div className="flex min-w-0 flex-wrap items-center gap-1">
                  <Input className="min-w-0 flex-1 basis-36" value={draft} disabled={busy || category.id === 'character'}
                    aria-label={t('lore.categories.rename', { name: current })}
                    onChange={(event) => setDrafts((prev) => ({ ...prev, [category.id]: event.target.value }))} />
                  {draft !== current && <Button size="icon-sm" variant="ghost" disabled={busy || !draft.trim()}
                    aria-label={t('lore.categories.saveName', { name: current })} title={t('lore.categories.saveName', { name: current })}
                    onClick={() => void mutate({ op: 'rename', id: category.id, name: draft })}><Check /></Button>}
                  <Button size="icon-sm" variant="ghost" disabled={busy || index === 0} aria-label={t('lore.categories.up', { name: current })}
                    title={t('lore.categories.up', { name: current })}
                    onClick={() => void mutate({ op: 'move', id: category.id, index: index - 1 })}><ArrowUp /></Button>
                  <Button size="icon-sm" variant="ghost" disabled={busy || index === categories.length - 1} aria-label={t('lore.categories.down', { name: current })}
                    title={t('lore.categories.down', { name: current })}
                    onClick={() => void mutate({ op: 'move', id: category.id, index: index + 1 })}><ArrowDown /></Button>
                  <Button size="icon-sm" variant="ghost" disabled={busy || category.id === 'character' || !destinations.length}
                    aria-label={t('lore.categories.remove', { name: current })} title={t('lore.categories.remove', { name: current })}
                    onClick={() => { setRemoving(category.id); setDestination(destinations[0].id) }}><Trash2 /></Button>
                </div>
                {removing === category.id && <div className="flex min-w-0 flex-col gap-2 border-t pt-2">
                  <p className="text-sm text-muted-foreground">{t('lore.categories.removeHint')}</p>
                  <Select value={destination} onValueChange={setDestination} disabled={busy}>
                    <SelectTrigger className="w-full min-w-0" aria-label={t('lore.categories.destination')}><SelectValue /></SelectTrigger>
                    <SelectContent><SelectGroup>{destinations.map((c) => <SelectItem key={c.id} value={c.id}>{label(c.id)}</SelectItem>)}</SelectGroup></SelectContent>
                  </Select>
                  <div className="flex flex-wrap justify-end gap-2">
                    <Button size="sm" variant="ghost" disabled={busy} onClick={() => setRemoving('')}>{t('common.cancel')}</Button>
                    <Button size="sm" variant="destructive" disabled={busy || !destination}
                      onClick={() => void mutate({ op: 'delete', id: category.id, destination_id: destination })}>{t('lore.categories.confirmRemove')}</Button>
                  </div>
                </div>}
              </div>
            )
          })}
        </div>
        <form className="flex items-end gap-2" onSubmit={(event) => { event.preventDefault(); if (name.trim()) void mutate({ op: 'create', name }) }}>
          <Field className="min-w-0 flex-1">
            <FieldLabel htmlFor="lore-category-name">{t('lore.categories.name')}</FieldLabel>
            <Input id="lore-category-name" value={name} disabled={busy || isPending || isError} onChange={(event) => setName(event.target.value)} />
          </Field>
          <Button type="submit" disabled={busy || isPending || isError || !name.trim()}><Plus data-icon="inline-start" />{t('lore.categories.add')}</Button>
        </form>
        <DialogFooter><Button variant="outline" disabled={saving} onClick={() => setOpen(false)}>{t('common.close')}</Button></DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
