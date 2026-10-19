import { StrictMode } from 'react'
import { fireEvent, render, screen } from '@testing-library/react'
import { expect, it, vi } from 'vitest'
import { MarketEntryDetail } from './MarketEntryDetail'
import { discardPreview, previewSource, type MarketEntry } from './api'
vi.mock('./api', async (original) => ({ ...await original<typeof import('./api')>(), previewSource: vi.fn(), discardPreview: vi.fn() }))
const detail: MarketEntry = { id: 'mixed', name: { 'zh-CN': '混合包' }, description: { 'zh-CN': '写作与游戏资源' }, author: 'Author', kinds: ['preset.image'], tags: ['writing'], format: 'denova.resource-pack', updated_at: '2026-09-25', source: { kind: 'github', url: 'https://github.com/author/repo', ref: 'main', path: 'resources/mixed' } }
it('browses catalog metadata without downloading and delegates acquisition on demand', () => {
  vi.mocked(previewSource).mockClear()
  vi.mocked(discardPreview).mockClear()
  const onImport = vi.fn()
  const view = render(<StrictMode><MarketEntryDetail detail={detail} acquired={0} onBack={vi.fn()} onManage={vi.fn()} onImport={onImport} /></StrictMode>)
  expect(screen.getByRole('link', { name: 'github.com/author/repo' })).toBeVisible()
  expect(screen.getByText('· resources/mixed')).toBeVisible()
  expect(screen.queryByRole('checkbox')).not.toBeInTheDocument()
  expect(previewSource).not.toHaveBeenCalled()
  fireEvent.click(screen.getByRole('button', { name: '获取资源包' }))
  expect(onImport).toHaveBeenCalledWith(detail.source)
  view.unmount()
  expect(previewSource).not.toHaveBeenCalled()
  expect(discardPreview).not.toHaveBeenCalled()
})
