import { Node, type JSONContent } from '@tiptap/core'
import type { Node as ProseMirrorNode } from '@tiptap/pm/model'
import type { Editor } from '@tiptap/react'
import { Plugin, TextSelection } from '@tiptap/pm/state'

export const RAW_MARKDOWN_NODE = 'rawMarkdown'

/**
 * Literal Markdown source hosted by ProseMirror.
 *
 * It deliberately is not TipTap's normal code-block node: source mode must not
 * add Markdown fences or exit into rich paragraphs after repeated Enter presses.
 */
export const RawMarkdown = Node.create<{ isSourceMode: () => boolean }>({
  name: RAW_MARKDOWN_NODE,
  group: 'block',
  content: 'text*',
  marks: '',
  code: true,
  defining: true,
  isolating: true,
  addOptions() { return { isSourceMode: () => false } },
  addProseMirrorPlugins() {
    return [new Plugin({
      props: {
        handleDOMEvents: {
          beforeinput: (view, event) => {
            if (!this.options.isSourceMode() || event.isComposing || event.inputType !== 'insertText'
              || !event.data?.includes('\n')) return false
            // Chromium replaces a selected <pre> in several native DOM edits for
            // multiline input. Insert once so reconciliation cannot discard it.
            event.preventDefault()
            view.dispatch(view.state.tr.insertText(normalizeSourceLineEndings(event.data)).scrollIntoView())
            return true
          },
        },
      },
      appendTransaction: (_transactions, _previous, state) => {
        if (!this.options.isSourceMode()
          || (state.doc.childCount === 1 && state.doc.firstChild?.type === this.type)) return null
        // Replacing an AllSelection can produce the schema's default paragraph.
        // Preserve its literal text inside the source node before update/save runs.
        const text = state.doc.textBetween(0, state.doc.content.size, '\n')
        const caret = state.doc.textBetween(0, state.selection.head, '\n').length
        const node = this.type.create(null, text ? state.schema.text(text) : undefined)
        const transaction = state.tr.replaceWith(0, state.doc.content.size, node)
        return transaction.setSelection(TextSelection.create(transaction.doc, 1 + Math.min(caret, text.length)))
      },
    })]
  },
  parseHTML() {
    return [{ tag: 'pre[data-nova-raw-markdown]', preserveWhitespace: 'full' }]
  },
  renderHTML() {
    return [
      'pre',
      { 'data-nova-raw-markdown': 'true', spellcheck: 'false' },
      ['code', 0],
    ]
  },
  addKeyboardShortcuts() {
    return {
      // Keep native text replacement inside <code>; a document-wide selection
      // lets the browser remove the source container before ProseMirror reads it.
      'Mod-a': () => this.options.isSourceMode() && this.editor.commands.setTextSelection({
        from: 1, to: this.editor.state.doc.content.size - 1,
      }),
      Tab: () => this.editor.commands.insertContent('\t'),
    }
  },
})

/** Creates a schema-valid source document without parsing Markdown syntax. */
export function createMarkdownSourceDocument(value: string): JSONContent {
  const source = normalizeSourceLineEndings(value)
  return {
    type: 'doc',
    content: [{
      type: RAW_MARKDOWN_NODE,
      ...(source ? { content: [{ type: 'text', text: source }] } : {}),
    }],
  }
}

/** Returns the literal source and its first ProseMirror text position. */
export function readMarkdownSourceDocument(
  value: Editor | ProseMirrorNode,
): { content: string; contentStart: number } | null {
  const document = 'state' in value ? value.state.doc : value
  let result: { content: string; contentStart: number } | null = null
  document.descendants((node, position) => {
    if (result || node.type.name !== RAW_MARKDOWN_NODE) return
    result = { content: node.textContent, contentStart: position + 1 }
    return false
  })
  return result
}

function normalizeSourceLineEndings(value: string): string {
  return value.replace(/\r\n?/g, '\n')
}
