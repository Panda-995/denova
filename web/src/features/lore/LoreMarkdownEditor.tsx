import { useEffect, useMemo, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import type { Editor } from '@tiptap/core'
import { exitSuggestion, type SuggestionProps } from '@tiptap/suggestion'
import { useTranslation } from 'react-i18next'
import { MarkdownContentEditor, type MarkdownContentEditorProps } from '@/components/Editor/MarkdownContentEditor'
import { FileReferencePicker, type FileReferencePickerHandle } from '@/components/Chat/FileReferencePicker'
import type { LoreItem } from '@/lib/api'
import { createLoreReferenceExtension, loreNameKey, loreSuggestionKey } from './lore-reference'

/** Shared editable/read-only Lore Markdown surface. Opening references never replaces a draft. */
export function LoreMarkdownEditor({ items, onOpenReference, ...props }: MarkdownContentEditorProps & {
  items: LoreItem[]
  onOpenReference: (name: string) => void
}) {
  const { t } = useTranslation()
  const [suggestion, setSuggestion] = useState<SuggestionProps | null>(null)
  const picker = useRef<FileReferencePickerHandle>(null)
  const editorRef = useRef<Editor | null>(null)
  const current = useRef({ items, onOpenReference, t })
  current.current = { items, onOpenReference, t }
  const extensions = useMemo(() => [createLoreReferenceExtension({
    describe: name => {
      const item = current.current.items.find(item => loreNameKey(item.name) === loreNameKey(name))
      return {
        missing: !item,
        label: current.current.t(item ? (item.enabled ? 'lore.references.open' : 'lore.references.openDisabled') : 'lore.references.missing', { name }),
      }
    },
    onOpen: name => current.current.onOpenReference(name),
    onSuggest: setSuggestion,
    onEditor: editor => { editorRef.current = editor },
    onKeyDown: event => {
      if (event.key === 'ArrowUp' || event.key === 'ArrowDown') return picker.current?.moveActive(event.key === 'ArrowUp' ? -1 : 1) ?? false
      if (event.key === 'Enter' || event.key === 'Tab') return picker.current?.selectActive() ?? false
      return false
    },
  })], [])
  // A catalog change refreshes missing/disabled decorations without changing content.
  useEffect(() => {
    const editor = editorRef.current
    if (editor && !editor.isDestroyed) editor.view.dispatch(editor.state.tr)
  }, [items, t])
  const rect = suggestion?.clientRect?.()
  return (
    <div className="relative flex min-h-0 min-w-0 flex-1 flex-col">
      <MarkdownContentEditor {...props} extensions={extensions} />
      {suggestion && !props.readOnly && createPortal(
        <div style={{ position: 'fixed', left: rect?.left ?? 0, top: rect?.top ?? 0 }}>
          <FileReferencePicker
            ref={picker} open query={suggestion.query}
            items={items.map(item => ({ value: item.name, label: item.name, description: item.enabled ? item.brief_description : t('lore.references.disabled'), kind: 'lore' }))}
            heading={t('lore.references.insert')} placeholder={t('lore.references.search')} emptyText={t('lore.references.noMatches')}
            onSelect={item => {
              const { editor, range } = suggestion
              exitSuggestion(editor.view, loreSuggestionKey)
              editor.chain().focus().insertContentAt(range, props.mode === 'source'
                ? { type: 'text', text: `[[${item.value}]]` }
                : { type: 'loreReference', attrs: { name: item.value } }).run()
            }}
          />
        </div>, document.body,
      )}
    </div>
  )
}
