import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ExportDialog } from './ExportDialog'
import { exchange, type Installation, type ExportResource } from './api'

vi.mock('@/lib/api', () => ({ getBooks: vi.fn().mockResolvedValue([]) }))
vi.mock('./api', async (importOriginal) => ({
  ...await importOriginal<typeof import('./api')>(),
  exchange: vi.fn(),
}))

const choices: ExportResource[] = [
  { local: { kind: 'preset.narrative', scope: 'global', id: 'steady' }, name: 'Steady' },
  { local: { kind: 'preset.image', scope: 'global', id: 'ink' }, name: 'Ink' },
  { local: { kind: 'preset.narrative', scope: 'global', id: 'brisk' }, name: 'Brisk', description: 'Fast pacing' },
]

beforeEach(() => {
  vi.mocked(exchange).mockReset().mockImplementation(async (path) => {
    if (path === '/export-resources') return choices
    if (path === '/export-definitions') return []
    if (path === '/exports') return { resources: [], files: 2, bytes: 100 }
    throw new Error(`Unexpected exchange path: ${path}`)
  })
})

it('groups interleaved resources and submits the exact selection after bulk and partial changes', async () => {
  render(<ExportDialog onClose={vi.fn()} />)
  const narrative = await screen.findByRole('group', { name: '叙事风格' })
  const header = within(narrative).getByRole('button', { name: /叙事风格/ })
  expect(header).toHaveAttribute('aria-expanded', 'false')
  header.focus()
  fireEvent.keyDown(header, { key: 'ArrowDown' })
  expect(screen.getByRole('button', { name: '图像方案' })).toHaveFocus()
  expect(screen.queryByRole('checkbox', { name: 'Steady' })).not.toBeInTheDocument()
  fireEvent.click(header)
  fireEvent.click(within(narrative).getByRole('checkbox', { name: '全选叙事风格' }))
  expect(screen.getByRole('checkbox', { name: 'Steady' })).toBeChecked()
  expect(screen.getByRole('checkbox', { name: 'Brisk' })).toBeChecked()
  fireEvent.click(screen.getByRole('checkbox', { name: 'Brisk' }))
  expect(within(narrative).getByRole('checkbox', { name: '全选叙事风格' })).toBePartiallyChecked()
  fireEvent.click(header)
  expect(header).toHaveAttribute('aria-expanded', 'false')
  expect(header).toHaveTextContent('已选 1 / 2')
  fireEvent.change(screen.getByLabelText('资源包名称'), { target: { value: 'Grouped export' } })
  fireEvent.click(screen.getByRole('button', { name: '预览导出' }))
  await waitFor(() => expect(exchange).toHaveBeenCalledWith('/exports', expect.objectContaining({
    resources: [choices[0].local],
  })))
})

it('limits bulk actions to search matches, preserves hidden selections, and restores collapsed groups', async () => {
  render(<ExportDialog initialResources={[choices[0].local, choices[1].local]} onClose={vi.fn()} />)
  await screen.findByText('已选 2 项')
  const search = screen.getByRole('textbox', { name: '搜索名称、描述或类型' })
  fireEvent.change(search, { target: { value: '  fast PACING  ' } })
  expect(screen.queryByRole('group', { name: '图像方案' })).not.toBeInTheDocument()
  expect(screen.getByRole('checkbox', { name: 'Brisk' })).toBeVisible()
  const matchingGroup = screen.getByRole('checkbox', { name: '全选叙事风格的搜索结果' })
  fireEvent.click(matchingGroup)
  expect(screen.getByText('已选 3 项')).toBeVisible()
  fireEvent.click(matchingGroup)
  expect(screen.getByText('已选 2 项')).toBeVisible()
  fireEvent.change(search, { target: { value: '图像方案' } })
  expect(screen.getByRole('checkbox', { name: 'Ink' })).toBeChecked()
  fireEvent.change(search, { target: { value: 'missing resource' } })
  expect(screen.getByText('没有匹配的内容，已有选择保持不变。')).toBeVisible()
  expect(screen.getByText('已选 2 项')).toBeVisible()
  fireEvent.change(search, { target: { value: '' } })
  const narrative = screen.getByRole('group', { name: '叙事风格' })
  expect(within(narrative).getByRole('button')).toHaveAttribute('aria-expanded', 'false')
  expect(within(narrative).getByRole('checkbox')).toBePartiallyChecked()
  fireEvent.click(screen.getByRole('button', { name: '清空选择' }))
  expect(screen.getByText('已选 0 项')).toBeVisible()
  expect(screen.getByRole('button', { name: '预览导出' })).toBeDisabled()
})

it('shows an empty library without offering an export', async () => {
  vi.mocked(exchange).mockResolvedValue([])
  render(<ExportDialog onClose={vi.fn()} />)
  expect(await screen.findByText('暂无可导出的资源')).toBeVisible()
  expect(screen.getByRole('button', { name: '预览导出' })).toBeDisabled()
})

describe.each([['lore.collection', '资料集合', '项目全部资料'], ['game.openings', '开场白集合', '作品中的全部开场白']] as const)('%s export scope', (kind, label, projectLabel) => {
  const all = { kind, scope: 'project', project_id: 'book', id: 'all' }
  const acquired = { ...all, id: 'acquired' }
  beforeEach(() => {
    vi.mocked(exchange).mockImplementation(async path => path.startsWith('/export-resources') ? [
      { local: all, name: 'Lore', item_count: 310 },
      { local: acquired, name: 'Imported lore', item_count: 300 },
    ] : [])
  })
  it('offers the whole project collection without overlapping acquired subsets', async () => {
    render(<ExportDialog projectID="book" onClose={vi.fn()} />)
    await waitFor(() => expect(screen.getByRole('checkbox', { name: `全选${label}` })).toBeEnabled())
    fireEvent.click(screen.getByRole('button', { name: label }))
    expect(screen.getByRole('checkbox', { name: projectLabel })).toBeVisible()
    expect(screen.queryByText('Imported lore')).not.toBeInTheDocument()
  })
  it('reexports an acquired collection without implicitly including unrelated project lore', async () => {
    const installation = { installation_id: 'installed', package: { id: 'pack', name: 'Imported lore' }, source: { kind: 'file' }, tracking: 'tracked', update_mode: 'manual', updated_at: '', project_id: 'book', bindings: [{ resource_id: 'lore', local: acquired, ownership: 'owned', source_digest: '1' }] } as Installation
    render(<ExportDialog installation={installation} onClose={vi.fn()} />)
    await waitFor(() => expect(screen.getByRole('checkbox', { name: `全选${label}` })).toBeEnabled())
    fireEvent.click(screen.getByRole('button', { name: label }))
    expect(screen.getByRole('checkbox', { name: /Imported lore/ })).toBeChecked()
    expect(screen.queryByText(projectLabel)).not.toBeInTheDocument()
  })

})

describe('package presentation metadata', () => {
  const info = {
    id: 'localized', name: '中文资源包', description: '中文简介', author: 'Author', version: '1.0.0',
    locale: 'zh-CN', usage: '导入后选用', compatibility: '需要模型', tags: ['writing'],
    translations: { 'en-US': { name: 'Creative kit', description: 'Writing resources', usage: 'Select after import' } },
  }
  it('preserves the full manifest when reexporting an installation', async () => {
    const installation = {
      installation_id: 'installed', package: info, source: { kind: 'file' },
      tracking: 'tracked', update_mode: 'manual', updated_at: '',
      bindings: [{ resource_id: 'narrative', local: choices[0].local, ownership: 'owned', source_digest: '1' }],
    } as Installation
    render(<ExportDialog installation={installation} onClose={vi.fn()} />)
    await screen.findByText('已选 1 项')
    fireEvent.click(screen.getByRole('button', { name: '预览导出' }))
    await waitFor(() => expect(exchange).toHaveBeenCalledWith('/exports', expect.objectContaining({ package: info })))
  })
  it('preserves the full manifest when reusing a saved export definition', async () => {
    const definition = { package: info, resources: [choices[0].local], revision: '1' }
    vi.mocked(exchange).mockImplementation(async (path, _body, method) => {
      if (path === '/export-definitions') return method === 'PUT' ? definition : [definition]
      if (path === '/export-resources') return choices
      return { resources: [], files: 2, bytes: 100 }
    })
    render(<ExportDialog onClose={vi.fn()} />)
    await screen.findByText('已保存的导出组合')
    fireEvent.keyDown(screen.getAllByRole('combobox')[0], { key: 'Enter' })
    fireEvent.click(await screen.findByRole('option', { name: info.name }))
    await screen.findByText('已选 1 项')
    fireEvent.click(screen.getByRole('button', { name: '预览导出' }))
    await waitFor(() => expect(exchange).toHaveBeenCalledWith('/exports', expect.objectContaining({ package: info })))
    await waitFor(() => expect(exchange).toHaveBeenCalledWith('/export-definitions', expect.objectContaining({ package: info }), 'PUT'))
  })
})
