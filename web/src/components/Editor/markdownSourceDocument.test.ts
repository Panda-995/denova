// @vitest-environment jsdom
import { Editor } from '@tiptap/core'
import StarterKit from '@tiptap/starter-kit'
import { closeHistory } from '@tiptap/pm/history'
import { describe, expect, it } from 'vitest'
import { RawMarkdown, createMarkdownSourceDocument, readMarkdownSourceDocument } from './markdownSourceDocument'

describe('literal Markdown source editing', () => {
  it('preserves all-selection replacement, deletion and undo without intercepting a switch to rich mode', () => {
    let sourceMode = true
    const editor = new Editor({
      extensions: [StarterKit.configure({ trailingNode: { notAfter: ['rawMarkdown'] } }), RawMarkdown.configure({ isSourceMode: () => sourceMode })],
      content: createMarkdownSourceDocument('Old body.'),
    })
    try {
      const content = 'Updated [[白塔议会]]\n\n`[[Code]]`'
      editor.commands.selectAll()
      editor.commands.insertContent({ type: 'text', text: content })
      expect(readMarkdownSourceDocument(editor)?.content).toBe(content)
      editor.view.dispatch(closeHistory(editor.state.tr))
      editor.commands.selectAll()
      editor.commands.deleteSelection()
      expect(readMarkdownSourceDocument(editor)?.content).toBe('')
      editor.commands.undo()
      expect(readMarkdownSourceDocument(editor)?.content).toBe(content)
      sourceMode = false
      editor.commands.setContent({ type: 'doc', content: [{ type: 'paragraph', content: [{ type: 'text', text: 'Rich text' }] }] })
      expect(readMarkdownSourceDocument(editor)).toBeNull()
      expect(editor.getText()).toBe('Rich text')
    } finally { editor.destroy() }
  })
})
