import { fireEvent, render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { CollisionDetection } from '@dnd-kit/core'
import { controlSectionCollision, SortableControlSections } from './SortableControlSections'
import { ControlSection } from './StoryTuningControls'

const key = 'nova.interactive.controlSections.v1'
function cards(action = vi.fn()) {
  return <SortableControlSections>
    <ControlSection key="first" icon={null} title="First" action={<button onClick={action}>Configure</button>}><input aria-label="Draft" defaultValue="keep me" /></ControlSection>
    <ControlSection key="second" icon={null} title="Second"><span>Second content</span></ControlSection>
  </SortableControlSections>
}

describe('control section layout', () => {
  beforeEach(() => localStorage.removeItem(key))

  it('toggles from the title, retains draft fields and keeps header actions independent', () => {
    const action = vi.fn()
    const { unmount } = render(cards(action))
    fireEvent.change(screen.getByLabelText('Draft'), { target: { value: 'unsaved draft' } })
    fireEvent.click(screen.getByRole('button', { name: 'Configure' }))
    expect(action).toHaveBeenCalledOnce()
    expect(screen.getByRole('button', { name: 'First' })).toHaveAttribute('aria-expanded', 'true')
    fireEvent.click(screen.getByRole('button', { name: 'First' }))
    expect(screen.getByLabelText('Draft')).not.toBeVisible()
    fireEvent.click(screen.getByRole('button', { name: 'First' }))
    expect(screen.getByLabelText('Draft')).toHaveValue('unsaved draft')
    fireEvent.click(screen.getByRole('button', { name: 'First' }))
    unmount()
    render(cards())
    expect(screen.getByRole('button', { name: 'First' })).toHaveAttribute('aria-expanded', 'false')
  })

  it('restores order while dropping obsolete IDs, removing duplicates and including new cards', () => {
    localStorage.setItem(key, JSON.stringify({ order: ['removed', 'second', 'second'], collapsed: [] }))
    const { container } = render(cards())
    expect(Array.from(container.querySelectorAll('[data-control-section]'), element => element.getAttribute('data-control-section'))).toEqual(['second', 'first'])
  })
})


describe('expanded control section collision', () => {
  const rect = (top: number, height: number) => ({ top, bottom: top + height, left: 0, right: 300, width: 300, height })
  const target = rect(135, 300)
  const active = rect(443, 265)
  const args: Parameters<CollisionDetection>[0] = {
    active: { id: 'checks', data: { current: {} }, rect: { current: { initial: active, translated: rect(331, 265) } } },
    collisionRect: rect(331, 265),
    droppableRects: new Map([['agent', target], ['checks', active]]),
    droppableContainers: ['agent', 'checks'].map(id => ({ id, key: id, disabled: false, node: { current: null }, rect: { current: id === 'agent' ? target : active }, data: { current: {} } })),
    pointerCoordinates: { x: 100, y: 350 },
  }

  it('moves an expanded card upward when its title enters the preceding card', () => {
    // The dragged card center is still closer to its old position (the previous bug).
    expect(controlSectionCollision(args)[0].id).toBe('agent')
  })

  it('moves a short card downward into a tall card without reaching its center', () => {
    expect(controlSectionCollision({ ...args, collisionRect: rect(445, 38), pointerCoordinates: { x: 100, y: 460 } })[0].id).toBe('checks')
  })

  it('retains geometry-based sorting for keyboard moves without a pointer', () => {
    expect(controlSectionCollision({ ...args, pointerCoordinates: null, collisionRect: target })[0].id).toBe('agent')
  })

  it('moves a collapsed card upward into the bottom of an expanded keyboard target', () => {
    expect(controlSectionCollision({ ...args, pointerCoordinates: null,
      collisionRect: rect(397, 38),
      droppableRects: new Map([['agent', target], ['checks', rect(443, 38)]]),
    })[0].id).toBe('agent')
  })
})
