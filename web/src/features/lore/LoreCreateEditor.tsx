import { LoreIndexMemberships } from "./LoreIndexMemberships"
import type { LoreIndexMembership } from "@/lib/api-client/types"
import { useEffect, useId, useRef, useState } from 'react'
import { BookMarked, X } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { createProjectLoreItem, type LoreItem, type LoreItemInput } from '@/lib/api'
import { notifyLoreUpdated } from './events'

/** A title-only creation surface. No item or permanent identity exists until
 * the user finishes naming it; persisted editors never receive a temporary ID. */
export function LoreCreateEditor({
  projectId, category, categoryLabel, importance, loadMode, items, onCreated, onCancel,
}: {
  projectId: string
  category: string
  categoryLabel: string
  importance: LoreItemInput['importance']
  loadMode: LoreItemInput['load_mode']
  items: LoreItem[]
  onCreated: (item: LoreItem) => void
  onCancel: () => void
}) {
  const { t } = useTranslation()
  const inputId = useId()
  const input = useRef<HTMLInputElement>(null)
  const mounted = useRef(false)
  const submitting = useRef(false)
  const composing = useRef(false)
  const [name, setName] = useState('')
  const [memberships, setMemberships] = useState<LoreIndexMembership[]>([])
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  useEffect(() => {
    mounted.current = true
    // Dropdown menus restore focus when closing; focus the title afterwards.
    const frame = requestAnimationFrame(() => input.current?.focus())
    return () => { mounted.current = false; cancelAnimationFrame(frame) }
  }, [])

  const create = async () => {
    if (submitting.current || composing.current) return
    const title = name.trim()
    const validation = !/[\p{L}\p{Nd}]/u.test(title) || title.includes('[[') || title.includes(']]')
      ? t('lore.create.invalidName')
      : items.some((item) => item.name.trim().toLowerCase() === title.toLowerCase())
        ? t('lore.create.duplicateName') : ''
    if (validation) {
      setError(validation)
      input.current?.focus()
      return
    }
    submitting.current = true
    setSaving(true)
    setError('')
    try {
      const item = await createProjectLoreItem(projectId, {
        name: title, type: category, enabled: true, importance, load_mode: loadMode,
        tags: [], keywords: [], brief_description: '', content: '', index_memberships: memberships,
      })
      console.info('[lore-create] created item', { projectId, itemId: item.id })
      if (mounted.current) onCreated(item)
      else notifyLoreUpdated({ projectId })
    } catch (cause) {
      console.error('[lore-create] failed to create item', { projectId, category, cause })
      if (mounted.current) {
        setError(t('lore.create.failed'))
        input.current?.focus()
      }
    } finally {
      submitting.current = false
      if (mounted.current) setSaving(false)
    }
  }

  return (
    <div className="flex min-h-0 min-w-0 flex-1 flex-col bg-[var(--nova-bg)]" data-testid="lore-create-editor" aria-busy={saving}>
      <div className="flex shrink-0 items-start gap-2 border-b p-3">
        <BookMarked className="mt-2 size-4 shrink-0 text-muted-foreground" />
        <FieldGroup className="min-w-0 flex-1">
          <Field data-invalid={Boolean(error)}>
            <FieldLabel htmlFor={inputId} className="sr-only">{t('settingPanel.field.name')}</FieldLabel>
            <Input
              ref={input} id={inputId} value={name} readOnly={saving}
              aria-invalid={Boolean(error)} aria-describedby={`${inputId}-hint`}
              placeholder={t('lore.create.namePlaceholder', { category: categoryLabel })}
              onChange={(event) => { setName(event.target.value); setError('') }}
              onCompositionStart={() => { composing.current = true }}
              onCompositionEnd={() => { composing.current = false }}
              onKeyDown={(event) => {
                if (composing.current || event.nativeEvent.isComposing || event.keyCode === 229) return
                if (event.key === 'Escape' && !saving) { event.preventDefault(); onCancel() }
                if (event.key === 'Enter' || (event.key === 'Tab' && !event.shiftKey && name.trim())) {
                  event.preventDefault()
                  void create()
                }
              }}
            />
            <FieldDescription id={`${inputId}-hint`}>{t('lore.create.nameHint')}</FieldDescription>
            {error && <FieldError role="alert">{error}</FieldError>}
          </Field>
        </FieldGroup>
        <Button size="icon-sm" variant="ghost" disabled={saving} aria-label={t('common.cancel')} onClick={onCancel}><X /></Button>
      </div>
      <div className="w-full max-w-sm shrink-0 px-4 py-3"><LoreIndexMemberships projectId={projectId} memberships={memberships} onChange={setMemberships} /></div>
      <div className="flex min-h-0 flex-1 p-2 sm:p-3">
        <div
          role="textbox" aria-label={t('settingPanel.field.content')} aria-readonly="true"
          tabIndex={0} onFocus={() => { void create() }}
          className="min-h-32 min-w-0 flex-1 cursor-text rounded-md border p-3 text-sm text-muted-foreground outline-none focus-visible:ring-2 focus-visible:ring-ring"
        >
          {saving ? t('common.saving') : t('lore.create.contentHint')}
        </div>
      </div>
    </div>
  )
}
