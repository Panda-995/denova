import { describe, expect, it } from 'vitest'
import { searchInputCommands } from './input-command-search'

describe('searchInputCommands', () => {
  it.each([
    ['Code Review', 'core', true],
    ['code-review', 'CORE', true],
    ['code_review', 'core', true],
    ['plugin:code-review', 'core', true],
    ['code-review', 'cr', true],
    ['code-review', 'codereview', true],
    ['code-review', 'covi', false],
    ['code-review', 'cdre', false],
    ['code-quality-review', 'core', false],
    ['aa-ab', 'aab', true],
  ])('matches %s against %s using contiguous text or adjacent word prefixes', (name, query, matched) => {
    expect(searchInputCommands([name], query, (name) => name)).toEqual(matched ? [name] : [])
  })

  it('ranks by earliest match position across both match types, preserving ties', () => {
    const names = ['/check-core', '/code-review', '/core', '/other-code-review', '/code-review-core']
    expect(searchInputCommands(names, 'core', (name) => name)).toEqual([
      '/code-review', '/core', '/code-review-core', '/check-core', '/other-code-review',
    ])
  })
})
