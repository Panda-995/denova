import { useId, useRef, useState } from 'react'
import { Plus, X } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { InputGroup, InputGroupAddon, InputGroupButton, InputGroupInput } from '@/components/ui/input-group'
import { splitLoreTags } from './tags'

/** Token editing shared by both lore editors; the string boundary keeps their
 * existing autosave and conflict recovery contract intact. */
export function LoreTagsInput({ id, value, onChange, suggestions }: {
  id?: string
  value: string
  onChange: (value: string) => void
  suggestions: string[]
}) {
  const { t } = useTranslation()
  const listId = useId()
  const input = useRef<HTMLInputElement>(null)
  const composing = useRef(false)
  const [pending, setPending] = useState('')
  const tags = [...new Set(splitLoreTags(value))]
  const add = () => {
    if (composing.current || !pending.trim()) return
    onChange([...new Set([...tags, ...splitLoreTags(pending)])].join('，'))
    setPending('')
  }

  return <div className="min-w-0 flex-1" data-testid="lore-tags-input">
    <InputGroup className="h-auto min-h-8">
      <div className="flex min-w-0 flex-1 flex-wrap items-center gap-1 p-0.5">
        {tags.map(tag => <Badge key={tag} variant="secondary" className="h-auto max-w-full gap-0 py-0 pl-2 pr-0">
          <span className="min-w-0 whitespace-normal break-all">{tag}</span>
          <Button type="button" variant="ghost" size="icon-xs" aria-label={t('lore.tags.remove', { tag })}
            onMouseDown={event => event.preventDefault()}
            onClick={() => onChange(tags.filter(value => value !== tag).join('，'))}><X /></Button>
        </Badge>)}
        <InputGroupInput ref={input} id={id} value={pending} list={listId} className="h-6 min-w-0 basis-32 px-1"
          aria-label={t('settingPanel.field.tags')} placeholder={t('settingPanel.placeholder.tags')}
          onChange={event => setPending(event.target.value)} onBlur={add}
          onCompositionStart={() => { composing.current = true }}
          onCompositionEnd={() => { composing.current = false }}
          onKeyDown={event => {
            if (composing.current || event.nativeEvent.isComposing || event.keyCode === 229) return
            if (event.key === 'Enter') { event.preventDefault(); add() }
          }} />
      </div>
      <InputGroupAddon align="inline-end" className="shrink-0 py-0 has-[>button]:mr-0">
        <InputGroupButton type="button" disabled={!pending.trim()} aria-label={t('lore.tags.add')}
          onMouseDown={event => event.preventDefault()} onClick={() => { add(); input.current?.focus() }}><Plus /></InputGroupButton>
      </InputGroupAddon>
    </InputGroup>
    <datalist id={listId}>{[...new Set(suggestions)].filter(tag => !tags.includes(tag)).map(tag => <option key={tag} value={tag} />)}</datalist>
  </div>
}
