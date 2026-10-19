import { expect, test } from '../support/fixtures'
import { createAndOpenBook } from '../support/api'
import type { LoreItem } from '../../src/lib/api'

test('creates named lore in the writing workspace without losing the previous draft', async ({ page, request }) => {
  const book = await createAndOpenBook(request, 'Writing inline lore')
  const url = `/api/projects/${book.projectId}/book/lore/items`
  const items = async (): Promise<LoreItem[]> => (await (await request.get(url)).json()).items
  await page.setViewportSize({ width: 1920, height: 1080 })
  await page.goto('/')
  await page.getByLabel('工作台侧边栏').getByRole('button', { name: '写作', exact: true }).click()
  await page.getByTestId('book-settings-header-frame').getByRole('button', { name: '设定', exact: true }).click()
  const workspace = page.getByRole('region', { name: '作品设定', exact: true })
  await workspace.getByRole('button', { name: '创建第一条设定', exact: true }).click()
  const pending = workspace.getByTestId('lore-create-editor')
  const name = pending.getByLabel('名称', { exact: true })
  await expect(name).toBeFocused()
  expect(await items()).toEqual([])
  await name.fill('Writing Hero')
  await name.press('Enter')
  const body = workspace.getByRole('textbox', { name: '编辑设定：Writing Hero', exact: true })
  await expect(body).toBeFocused()
  await page.keyboard.insertText('Keep this draft when creating another entry.')
  await workspace.getByRole('button', { name: '新建地点', exact: true }).click()
  await expect(name).toBeFocused()
  expect((await items())[0]).toMatchObject({ id: 'writing_hero', content: expect.stringContaining('Keep this draft') })
  await name.press('Escape')
  await expect(pending).toHaveCount(0)
  expect(await items()).toHaveLength(1)
  await expect(body).toContainText('Keep this draft')

  await workspace.getByRole('button', { name: '新建地点', exact: true }).click()
  await name.fill('Writing+Hero')
  await name.press('Tab')
  await expect(workspace.getByRole('textbox', { name: '编辑设定：Writing+Hero', exact: true })).toBeFocused()
  expect((await items()).find(item => item.name === 'Writing+Hero')?.id).toBe('writing_hero-2')
  await page.reload()
  await expect(workspace.getByRole('textbox', { name: '编辑设定：Writing+Hero', exact: true })).toBeVisible()
  expect((await items()).find(item => item.id === 'writing_hero')?.content).toContain('Keep this draft')
})
