import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, expect, it, vi } from 'vitest'
import { getProjectLoreItems, type LoreItem } from '@/lib/api'
import { StoryBackgroundSelect } from './StoryBackgroundSelect'

vi.mock('@/lib/api', () => ({ getProjectLoreItems: vi.fn() }))

const location = {
  id: 'river', name: '长河', enabled: true,
  materials: { entries: [{ asset_id: 'day' }], cover_asset_id: 'day' },
  resolved_materials: [{ id: 'day', path: 'assets/day.png', name: '白昼', mime_type: 'image/png' }],
} as LoreItem

beforeEach(() => vi.mocked(getProjectLoreItems).mockReset())

it('loads on every opening so newly imported cover materials become selectable', async () => {
  const user = userEvent.setup()
  const onChange = vi.fn()
  vi.mocked(getProjectLoreItems).mockResolvedValueOnce([]).mockResolvedValueOnce([location])
  render(<StoryBackgroundSelect projectId="project-1" disabled={false} onChange={onChange} />)
  expect(getProjectLoreItems).not.toHaveBeenCalled()
  await user.click(screen.getByRole('button', { name: '当前背景' }))
  await waitFor(() => expect(getProjectLoreItems).toHaveBeenCalledTimes(1))
  await user.click(screen.getByRole('button', { name: '无背景' }))
  await user.click(screen.getByRole('button', { name: '当前背景' }))
  await user.click(await screen.findByRole('button', { name: /白昼.*长河/ }))
  expect(getProjectLoreItems).toHaveBeenCalledTimes(2)
  expect(onChange).toHaveBeenLastCalledWith({ item_id: 'river', asset_id: 'day', path: 'assets/day.png', name: '白昼' })
})

it('shows a retryable error and replaces stale results after a failed refresh', async () => {
  const user = userEvent.setup()
  vi.mocked(getProjectLoreItems).mockResolvedValueOnce([location]).mockRejectedValueOnce(new Error('Offline')).mockResolvedValueOnce([])
  render(<StoryBackgroundSelect projectId="project-1" disabled={false} onChange={vi.fn()} />)
  await user.click(screen.getByRole('button', { name: '当前背景' }))
  await user.click(await screen.findByRole('button', { name: /白昼.*长河/ }))
  await user.click(screen.getByRole('button', { name: '当前背景' }))
  await screen.findByRole('alert')
  expect(screen.queryByRole('button', { name: /白昼.*长河/ })).not.toBeInTheDocument()
  await user.click(screen.getByRole('button', { name: '重试' }))
  await waitFor(() => expect(screen.queryByRole('alert')).not.toBeInTheDocument())
  expect(getProjectLoreItems).toHaveBeenCalledTimes(3)
  expect(screen.queryByRole('button', { name: /白昼.*长河/ })).not.toBeInTheDocument()
})
