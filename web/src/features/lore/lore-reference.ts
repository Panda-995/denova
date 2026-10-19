import { Node, nodeInputRule, type Editor, type JSONContent } from '@tiptap/core'
import type { Node as ProseMirrorNode } from '@tiptap/pm/model'
import { MarkdownManager } from '@tiptap/markdown'
import StarterKit from '@tiptap/starter-kit'
import { TableKit } from '@tiptap/extension-table'
import { Plugin, PluginKey } from '@tiptap/pm/state'
import { Decoration, DecorationSet } from '@tiptap/pm/view'
import Suggestion, { type SuggestionProps } from '@tiptap/suggestion'
import { RAW_MARKDOWN_NODE } from '@/components/Editor/markdownSourceDocument'
import { createMarkdownParser } from '@/components/Editor/markdownParser'

export const loreNameKey = (name: string) => name.trim().toLowerCase()

/** Names, scoped to the current project, are the entire reference identity.
 * Markdown parsing owns code/escape handling; no relation records are persisted. */
export const LoreReference = Node.create({
  name: 'loreReference',
  group: 'inline',
  inline: true,
  atom: true,
  selectable: true,
  extendNodeSchema(extension) {
    // Text selections and review anchors must include the visible reference name.
    return extension.name === 'loreReference' ? { leafText: (node: ProseMirrorNode) => node.attrs.name } : {}
  },
  addAttributes() {
    return { name: { default: '', parseHTML: element => element.getAttribute('data-lore-reference') } }
  },
  parseHTML() { return [{ tag: 'span[data-lore-reference]' }] },
  renderHTML({ node }) {
    return ['span', {
      'data-lore-reference': node.attrs.name,
      role: 'button', tabindex: '0',
      class: 'cursor-pointer rounded-sm text-primary underline decoration-dotted underline-offset-4 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring',
    }, node.attrs.name]
  },
  renderText({ node }) { return `[[${node.attrs.name}]]` },
  addInputRules() {
    return [nodeInputRule({
      find: /(?<!\\)\[\[((?:(?!\[\[|\]\])[^\n])+(?:\](?=\]\]))?)\]\]$/,
      type: this.type,
      getAttributes: match => match[1].trim() ? { name: match[1].trim() } : false,
    })]
  },
  markdownTokenizer: {
    name: 'loreReference',
    level: 'inline',
    start: source => source.indexOf('[['),
    tokenize(source) {
      const match = /^\[\[((?:(?!\[\[|\]\])[\s\S])+(?:\](?=\]\]))?)\]\]/.exec(source)
      if (!match || !match[1].trim()) return undefined
      return { type: 'loreReference', raw: match[0], name: match[1].trim() }
    },
  },
  parseMarkdown: (token, helpers) => helpers.createNode('loreReference', { name: token.name }),
  renderMarkdown: node => `[[${node.attrs?.name}]]`,
})

const parser = new MarkdownManager({ marked: createMarkdownParser(), extensions: [StarterKit, TableKit, LoreReference] })

/** Uses the editor's parser so backlinks and visible links agree, including escapes and tables. */
export function loreReferenceNames(content: string): string[] {
  const names = new Map<string, string>()
  const visit = (node: JSONContent) => {
    if (node.type === LoreReference.name) {
      const name = String(node.attrs?.name || '')
      names.set(loreNameKey(name), name)
    }
    node.content?.forEach(visit)
  }
  visit(parser.parse(content))
  return [...names.values()]
}

export const loreSuggestionKey = new PluginKey('loreSuggestion')

/** Live callbacks keep catalog updates out of editor reconstruction and undo history. */
export function createLoreReferenceExtension(options: {
  describe: (name: string) => { label: string; missing: boolean }
  onOpen: (name: string) => void
  onSuggest: (props: SuggestionProps | null) => void
  onKeyDown: (event: KeyboardEvent) => boolean
  onEditor: (editor: Editor | null) => void
}) {
  return LoreReference.extend({
    onCreate({ editor }) { options.onEditor(editor) },
    onDestroy() { options.onEditor(null) },
    addProseMirrorPlugins() {
      const openTarget = (event: Event) => {
        const target = event.target instanceof Element ? event.target.closest('[data-lore-reference]') : null
        if (!target) return false
        event.preventDefault()
        options.onOpen(target.getAttribute('data-lore-reference') || '')
        return true
      }
      return [
        new Plugin({
          props: {
            decorations(state) {
              const decorations: Decoration[] = []
              state.doc.descendants((node, position) => {
                if (node.type.name !== LoreReference.name) return
                const { label, missing } = options.describe(node.attrs.name)
                decorations.push(Decoration.node(position, position + node.nodeSize, {
                  title: label, 'aria-label': label,
                  'data-reference-missing': String(missing),
                  ...(missing ? { class: 'text-destructive decoration-wavy' } : {}),
                }))
              })
              return DecorationSet.create(state.doc, decorations)
            },
            handleDOMEvents: {
              click: (_view, event) => openTarget(event),
              keydown: (_view, event) => (event.key === 'Enter' || event.key === ' ') && openTarget(event),
            },
          },
        }),
        Suggestion({
          editor: this.editor,
          pluginKey: loreSuggestionKey,
          char: '@',
          allowSpaces: true,
          allowedPrefixes: null,
          allow: ({ state }) => {
            const { $from } = state.selection
            return (!$from.parent.type.spec.code || $from.parent.type.name === RAW_MARKDOWN_NODE)
              && !$from.marks().some(mark => mark.type.spec.code)
          },
          render: () => ({
            onStart: options.onSuggest,
            onUpdate: options.onSuggest,
            onExit: () => options.onSuggest(null),
            onKeyDown: ({ event }) => !event.isComposing && event.keyCode !== 229 && options.onKeyDown(event),
          }),
        }),
      ]
    },
  })
}
