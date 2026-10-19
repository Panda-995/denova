import { TextSelection } from '@tiptap/pm/state'
import type { EditorView } from '@tiptap/pm/view'

let graphemes: Intl.Segmenter | undefined

/** Safari text deletion must reach editor state even when native DOM edits are
 * lost. Existing handlers retain selections, shortcuts, IME, atoms and joins. */
export function deleteSafariComposerText(view: EditorView, event: KeyboardEvent): boolean {
  if (event.key !== 'Backspace' && event.key !== 'Delete') return false
  if (navigator.vendor !== 'Apple Computer, Inc.' || typeof Intl.Segmenter !== 'function') return false
  if (view.composing || event.isComposing || event.keyCode === 229) return false
  if (event.metaKey || event.ctrlKey || event.altKey || event.shiftKey) return false

  const { selection } = view.state
  if (!(selection instanceof TextSelection) || !selection.empty) return false
  const $pos = selection.$from
  const backward = event.key === 'Backspace'
  const adjacent = backward ? $pos.nodeBefore : $pos.nodeAfter
  if (!adjacent?.isText) return false

  // A single placeholder per inline leaf preserves ProseMirror offsets while
  // allowing graphemes to span text nodes with different formatting marks.
  const text = $pos.parent.textBetween(0, $pos.parent.content.size, '', '\ufffc')
  graphemes ??= new Intl.Segmenter(undefined, { granularity: 'grapheme' })
  const offset = $pos.parentOffset - (backward ? 1 : 0)
  // WebKit can misreport Segments.containing() around surrogate pairs; its
  // iterator supplies the correct boundaries without merging adjacent text.
  for (const segment of graphemes.segment(text)) {
    const end = segment.index + segment.segment.length
    if (end <= offset) continue
    let textOnly = true
    $pos.parent.nodesBetween(segment.index, end, node => { textOnly &&= node.isText })
    if (!textOnly) return false

    view.dispatch(view.state.tr.delete($pos.start() + segment.index, $pos.start() + end).scrollIntoView())
    return true
  }
  return false
}
