import { describe, expect, it } from 'vitest'
import { buildStoryStageCommandMenu } from './story-stage-commands'

const labels = { compactDescription: 'Compact context', compactHint: 'Run immediately' }
const skills = [
  { name: 'google-slides' },
  { name: 'check-ideas' },
  { name: 'lark-slides' },
  { name: 'other-ideas' },
  { name: 'Ideate' },
  { name: 'interface-design' },
  { name: 'index', description: 'ide in the description only' },
]

describe('buildStoryStageCommandMenu', () => {
  it('ranks name matches by position and preserves equal-position order', () => {
    const menu = buildStoryStageCommandMenu('IDE', skills, labels)
    expect(menu.commands.map(({ name }) => name)).toEqual([
      'Ideate', 'interface-design', 'check-ideas', 'other-ideas', 'lark-slides', 'google-slides',
    ])
    expect(menu.skillItems.map(({ index }) => index)).toEqual([0, 1, 2, 3, 4, 5])
    expect(menu.builtInItems).toEqual([])
    expect(buildStoryStageCommandMenu('idx', skills, labels).commands).toEqual([])
  })

  it('matches adjacent word prefixes without accepting arbitrary subsequences', () => {
    const skills = [{ name: 'code-review' }]
    expect(buildStoryStageCommandMenu('core', skills, labels).commands.map(({ name }) => name)).toEqual(['code-review'])
    expect(buildStoryStageCommandMenu('covi', skills, labels).commands).toEqual([])
  })

  it('keeps built-ins before ranked Skills with indexes matching the rendered order', () => {
    const menu = buildStoryStageCommandMenu('act', [
      { name: 'react' }, { name: 'action' }, { name: 'compact' }, { name: 'action' },
    ], labels)
    expect(menu.commands.map(({ name }) => name)).toEqual(['compact', 'action', 'react'])
    expect([...menu.builtInItems, ...menu.skillItems]).toEqual(
      menu.commands.map((command, index) => ({ command, index })),
    )
  })

  it('preserves catalog order for an empty query and hides suggestions without a trigger', () => {
    expect(buildStoryStageCommandMenu('', skills, labels).commands.map(({ name }) => name)).toEqual([
      'compact', ...skills.map(({ name }) => name),
    ])
    expect(buildStoryStageCommandMenu(null, skills, labels)).toEqual({ commands: [], builtInItems: [], skillItems: [] })
  })
})
