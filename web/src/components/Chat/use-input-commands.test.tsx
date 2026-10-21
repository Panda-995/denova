import { act, renderHook } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import type { AgentQuickPromptSettings } from '@/features/settings/types'
import { inputCommandValue } from './InputCommandMenu'
import { useInputCommands, type CommandScope } from './use-input-commands'

const quickPrompts: AgentQuickPromptSettings[] = [
  { id: 'first', name: '/new', prompt: 'Discuss the scene', enabled: true, behavior: 'send' },
  { id: 'second', name: '/new', prompt: 'Review the scene', enabled: true, behavior: 'fill' },
  { id: 'hidden', name: 'Hidden prompt', prompt: 'Hidden', enabled: false, behavior: 'fill' },
  { id: 'ideas', name: '梳理思路', prompt: 'Explore ideas', enabled: true, behavior: 'fill' },
]

describe('useInputCommands', () => {
  it('keeps duplicate prompt names separate from commands and Skills, preserving prompt identity', () => {
    const { result } = renderHook(() => useInputCommands({
      scope: 'all', skills: [{ name: 'new', description: 'Reserved' }, { name: 'review', description: 'Review Skill' }],
      quickPrompts, goalEnabled: false,
    }))
    act(() => result.current.setCommandQuery(''))
    const options = result.current.filteredCommands
    expect(new Set(options.map(inputCommandValue)).size).toBe(options.length)
    expect(result.current.filteredQuickPromptCommands.map(({ command }) => command)).toMatchObject([
      { source: 'quick-prompt', id: 'first', prompt: 'Discuss the scene' },
      { source: 'quick-prompt', id: 'second', prompt: 'Review the scene' },
      { source: 'quick-prompt', id: 'ideas', prompt: 'Explore ideas' },
    ])
    expect(result.current.filteredSkillCommands.map(({ command }) => command.cmd)).toEqual(['/review'])
    expect(result.current.filteredBuiltinCommands.some(({ command }) => command.cmd === '/goal')).toBe(false)
    expect(options.some((command) => command.cmd === 'Hidden prompt')).toBe(false)
  })

  it('searches prompt names by substring and reindexes the visible results', () => {
    const { result } = renderHook(() => useInputCommands({
      scope: 'all', skills: [], quickPrompts, goalEnabled: false,
    }))
    act(() => result.current.setCommandQuery('思路'))
    expect(result.current.filteredQuickPromptCommands).toMatchObject([
      { index: 0, command: { id: 'ideas', prompt: 'Explore ideas' } },
    ])
  })

  it('ranks name matches by position, ignoring case and preserving ties', () => {
    const { result } = renderHook(() => useInputCommands({
      scope: 'all',
      skills: [
        { name: 'google-slides', description: 'Slides' },
        { name: 'check-ideas', description: 'Ideas' },
        { name: 'lark-slides', description: 'Slides' },
        { name: 'other-ideas', description: 'Ideas' },
        { name: 'Ideate', description: 'Design' },
        { name: 'interface-design', description: 'Craft-first guidance' },
        { name: 'index', description: 'Non-contiguous letters' },
        { name: 'review', description: 'ide in the description only' },
      ],
      quickPrompts: [], goalEnabled: false,
    }))
    expect(result.current.filteredCommands).toEqual([])
    act(() => result.current.setCommandQuery('IDE'))
    expect(result.current.filteredSkillCommands.map(({ command, index }) => [command.cmd, index])).toEqual([
      ['/Ideate', 0], ['/interface-design', 1], ['/check-ideas', 2], ['/other-ideas', 3], ['/lark-slides', 4], ['/google-slides', 5],
    ])
    act(() => result.current.setCommandQuery('idx'))
    expect(result.current.filteredCommands).toEqual([])
  })

  it('matches adjacent word prefixes in prompt and Skill names without skipping letters within a word', () => {
    const { result } = renderHook(() => useInputCommands({
      scope: 'all', skills: [{ name: 'code-review', description: 'Review changes' }],
      quickPrompts: [{ id: 'review', name: 'Code Review', prompt: 'Review changes', enabled: true, behavior: 'fill' }],
      goalEnabled: false,
    }))
    act(() => result.current.setCommandQuery('core'))
    expect(result.current.filteredCommands.map(({ cmd }) => cmd)).toEqual(['Code Review', '/code-review'])
    act(() => result.current.setCommandQuery('covi'))
    expect(result.current.filteredCommands).toEqual([])
  })

  it('keeps group order and visible indexes aligned when substring matches are ranked', () => {
    const { result } = renderHook(() => useInputCommands({
      scope: 'all',
      skills: [{ name: 'status-report', description: '' }, { name: 'at', description: '' }],
      quickPrompts: [
        { id: 'late', name: 'Review status', prompt: 'Review', enabled: true, behavior: 'fill' },
        { id: 'early', name: 'At once', prompt: 'Continue', enabled: true, behavior: 'fill' },
      ],
      goalEnabled: false,
    }))
    act(() => result.current.setCommandQuery('at'))
    expect(result.current.filteredCommands.map(({ cmd }) => cmd)).toEqual([
      '/status', 'At once', 'Review status', '/at', '/status-report',
    ])
    expect([
      ...result.current.filteredBuiltinCommands,
      ...result.current.filteredQuickPromptCommands,
      ...result.current.filteredSkillCommands,
    ].map(({ command, index }) => [command, index])).toEqual(
      result.current.filteredCommands.map((command, index) => [command, index]),
    )
  })

  it.each<CommandScope>(['skills', 'none'])('keeps prompts out of the %s command scope', (scope) => {
    const { result } = renderHook(() => useInputCommands({
      scope, skills: [{ name: 'review', description: 'Review' }], quickPrompts, goalEnabled: false,
    }))
    act(() => result.current.setCommandQuery(''))
    expect(result.current.filteredQuickPromptCommands).toEqual([])
    expect(result.current.filteredCommands.map(({ cmd }) => cmd)).toEqual(scope === 'skills' ? ['/review'] : [])
  })
})
