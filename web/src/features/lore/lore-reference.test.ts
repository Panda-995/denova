import { describe, expect, it } from 'vitest'
import { MarkdownManager } from '@tiptap/markdown'
import StarterKit from '@tiptap/starter-kit'
import { TableKit } from '@tiptap/extension-table'
import { createMarkdownParser } from '@/components/Editor/markdownParser'
import { LoreReference, loreReferenceNames } from './lore-reference'

describe('Lore name references in Markdown', () => {
  it('finds names in prose and tables without treating code or escaped brackets as references', () => {
    expect(loreReferenceNames([
      'Lives in [[旧车站]], meets **[[White Tower]]** and [[white tower]].',
      '`[[Inline Code]]` and \\[[Escaped]].',
      '```md\n[[Fenced Code]]\n```',
      '| Place |\n| --- |\n| [[Station [East]]] |',
    ].join('\n\n'))).toEqual(['旧车站', 'white tower', 'Station [East]'])
  })

  it('preserves resolved and missing references through repeated rich/source round trips', () => {
    const manager = new MarkdownManager({ marked: createMarkdownParser(), extensions: [StarterKit, TableKit, LoreReference] })
    const source = 'Meets [[白塔议会]] and [[Missing *Name*]]. Literal \\[[Not a Link]].\n\n`[[Code]]`'
    const once = manager.serialize(manager.parse(source))
    const twice = manager.serialize(manager.parse(once))
    expect(twice).toBe(once)
    expect(loreReferenceNames(twice)).toEqual(['白塔议会', 'Missing *Name*'])
    expect(twice).toContain('[[Missing *Name*]]')
    expect(twice).toContain('`[[Code]]`')
  })

  it('does not install Lore tokenizers into unrelated Markdown parsers', () => {
    loreReferenceNames('[[Reference]]')
    const ordinary = new MarkdownManager({ marked: createMarkdownParser(), extensions: [StarterKit] })
    expect(ordinary.parse('[[Reference]]')).toMatchObject({
      content: [{ content: [{ type: 'text', text: '[[Reference]]' }] }],
    })
  })
})
