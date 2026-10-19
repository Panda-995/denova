import { Marked, type marked } from 'marked'

/** Tiptap accepts the global marked function's type but only calls parser methods.
 * Isolate custom tokenizers so Lore syntax cannot affect other Markdown surfaces. */
export function createMarkdownParser(): typeof marked {
  return new Marked() as unknown as typeof marked
}
