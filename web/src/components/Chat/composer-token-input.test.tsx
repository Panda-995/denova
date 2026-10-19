import { createRef, useLayoutEffect, useState } from 'react'
import { act, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ComposerTokenInput, type ComposerTokenInputHandle } from './composer-token-input'

it('preserves a new draft while a previous render synchronizes token suggestions', () => {
  const ref = createRef<ComposerTokenInputHandle>()
  const pendingSkills: string[] = []
  const readySkills = ['outline']
  function Composer({ suggestionsReady }: { suggestionsReady: boolean }) {
    const [value, setValue] = useState('')
    // Deliver an edit after commit but before that render's passive effects.
    useLayoutEffect(() => {
      if (suggestionsReady) ref.current!.replaceActiveTriggerText('草稿')
    }, [suggestionsReady])
    return <ComposerTokenInput ref={ref} value={value} onChange={setValue}
      knownSkills={suggestionsReady ? readySkills : pendingSkills} />
  }
  const view = render(<Composer suggestionsReady={false} />)
  view.rerender(<Composer suggestionsReady />)
  expect(ref.current!.getValue()).toBe('草稿')
  expect(screen.getByRole('textbox')).toHaveTextContent('草稿')
})

function mountComposer(initial: string, knownSkills: string[] = []) {
  const ref = createRef<ComposerTokenInputHandle>()
  const onTokenRemove = vi.fn()
  function Composer() {
    const [value, setValue] = useState(initial)
    return <ComposerTokenInput ref={ref} value={value} onChange={setValue}
      knownSkills={knownSkills} onTokenRemove={onTokenRemove} />
  }
  render(<Composer />)
  const input = screen.getByRole('textbox')
  return {
    input, ref, onTokenRemove,
    select: (start: number, end = start) => act(() => ref.current!.setSelectionRange(start, end)),
  }
}

describe('Safari composer deletion', () => {
  beforeEach(() => {
    vi.spyOn(navigator, 'vendor', 'get').mockReturnValue('Apple Computer, Inc.')
  })
  afterEach(() => vi.restoreAllMocks())

  // Synthetic key events do not perform a browser DOM edit. These assertions
  // require deletion to reach editor state without relying on native DOM recovery.
  it.each(['c', '文', '😀', '👩🏽‍💻', 'e\u0301'])('deletes one complete grapheme backward: %s', (character) => {
    const composer = mountComposer(`a${character}b`)
    composer.select(1 + character.length)
    fireEvent.keyDown(composer.input, { key: 'Backspace' })
    expect(composer.ref.current!.getValue()).toBe('ab')
  })

  it.each(['c', '文', '😀', '👩🏽‍💻', 'e\u0301'])('deletes one complete grapheme forward: %s', (character) => {
    const composer = mountComposer(`a${character}b`)
    composer.select(1)
    fireEvent.keyDown(composer.input, { key: 'Delete' })
    expect(composer.ref.current!.getValue()).toBe('ab')
  })

  it('keeps token deletion on its existing path', () => {
    const composer = mountComposer('/skill text', ['skill'])
    composer.select('/skill'.length)
    fireEvent.keyDown(composer.input, { key: 'Backspace' })
    expect(composer.ref.current!.getValue()).toBe(' text')
    expect(composer.onTokenRemove).toHaveBeenCalledExactlyOnceWith({ kind: 'skill', value: 'skill' })
  })

  it('deletes a whole grapheme when the caret is inside its surrogate pair', () => {
    const composer = mountComposer('a😀b')
    composer.select(2)
    fireEvent.keyDown(composer.input, { key: 'Backspace' })
    expect(composer.ref.current!.getValue()).toBe('ab')
  })

  it('keeps text deletion undoable', () => {
    const composer = mountComposer('abc')
    composer.select(3)
    fireEvent.keyDown(composer.input, { key: 'Backspace' })
    expect(composer.ref.current!.getValue()).toBe('ab')
    fireEvent.keyDown(composer.input, { key: 'z', ctrlKey: true })
    expect(composer.ref.current!.getValue()).toBe('abc')
  })

  it.each([
    { isComposing: true }, { keyCode: 229 }, { metaKey: true }, { ctrlKey: true },
    { altKey: true }, { shiftKey: true },
  ])('leaves composition and modified deletion to existing handlers: %j', (event) => {
    const composer = mountComposer('abc')
    composer.select(3)
    fireEvent.keyDown(composer.input, { key: 'Backspace', ...event })
    expect(composer.ref.current!.getValue()).toBe('abc')
  })

  it('does not replace non-Safari native text deletion', () => {
    vi.spyOn(navigator, 'vendor', 'get').mockReturnValue('Google Inc.')
    const composer = mountComposer('abc')
    composer.select(3)
    fireEvent.keyDown(composer.input, { key: 'Backspace' })
    expect(composer.ref.current!.getValue()).toBe('abc')
  })

  it('leaves selected text deletion to the editor keymap', () => {
    const composer = mountComposer('abc')
    composer.select(1, 3)
    fireEvent.keyDown(composer.input, { key: 'Backspace' })
    expect(composer.ref.current!.getValue()).toBe('a')
  })

  it.each([['Backspace', 2], ['Delete', 1]] as const)('preserves paragraph joining for %s', (key, position) => {
    const composer = mountComposer('a\nb')
    composer.select(position)
    fireEvent.keyDown(composer.input, { key })
    expect(composer.ref.current!.getValue()).toBe('ab')
  })
})
