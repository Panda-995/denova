import { expect, test } from '../support/fixtures'
import { createAndOpenBook } from '../support/api'
import type { LoreItem } from '../../src/lib/api'

for (const theme of ['dark', 'light']) {
  test(`shares library destinations and edits lore inline in ${theme}`, async ({ page, request }, testInfo) => {
    const book = await createAndOpenBook(request, `Writing lore navigation ${theme}`)
    const base = `/api/projects/${book.projectId}/book/lore`
    const items = async (): Promise<LoreItem[]> => (await (await request.get(`${base}/items`)).json()).items
    const index = async () => (await (await request.get(`${base}/index`)).json()).guide
    const settings = await (await request.get('/api/settings')).json()
    await request.patch('/api/settings', { data: { layer: 'user', base_revision: settings.revisions.user, changes: { theme, language: 'zh-CN' } } })
    await page.setViewportSize({ width: 1920, height: 1080 })
    await page.goto('/')
    const sidebar = page.getByLabel('工作台侧边栏')
    await sidebar.getByRole('button', { name: '写作', exact: true }).click()
    await page.getByTestId('book-settings-header-frame').getByRole('button', { name: '设定', exact: true }).click()
    const workspace = page.getByRole('region', { name: '作品设定', exact: true })
    const directory = workspace.locator('[data-slot="sidebar"]')
    await directory.getByRole('button', { name: '资料总览', exact: true }).click()
    const library = workspace.getByTestId('lore-library')
    await expect(library.getByText('还没有资料', { exact: true })).toBeVisible()
    await directory.getByRole('button', { name: '资料索引', exact: true }).click()
    await expect(workspace.getByTestId('lore-index-editor')).toBeVisible()
    await page.screenshot({ path: testInfo.outputPath(`workspace-${theme}-empty-index.png`) })

    const longName = '长名称LongName'.repeat(14)
    const longTag = 'LongUnbrokenTag'.repeat(12)
    for (const item of [
      { id: 'hero', name: '港口人物', type: 'character', tags: ['东陵', '主角'], brief_description: 'A brief that stays out of the directory.', content: 'Original body.' },
      { id: 'port', name: longName, type: 'location', tags: [], content: 'Port details.' },
    ]) {
      const response = await request.post(`${base}/items`, { data: item })
      expect(response.ok(), await response.text()).toBe(true)
    }
    await page.reload()
    await expect(directory.getByRole('button', { name: /^港口人物/ })).toBeVisible()
    await expect(directory).not.toContainText('A brief that stays out of the directory.')
    await directory.getByRole('button', { name: /^港口人物/ }).click()
    const name = workspace.getByRole('textbox', { name: '名称', exact: true })
    await expect(name).toHaveCount(0)
    const rename = workspace.getByRole('button', { name: '修改名称：港口人物', exact: true })
    await rename.click()
    await expect(name).toBeFocused()
    await name.fill('Discarded rename')
    await name.press('Escape')
    await expect(rename).toBeFocused()
    expect((await items()).find(item => item.id === 'hero')?.name).toBe('港口人物')
    await rename.click()
    await name.fill('  港口船长  ')
    await name.dispatchEvent('compositionstart')
    await name.press('Enter')
    await expect(name).toBeVisible()
    await name.dispatchEvent('compositionend')
    await name.press('Enter')
    await expect(workspace.getByRole('button', { name: '修改名称：港口船长', exact: true })).toBeFocused()
    await expect(name).toHaveCount(0)

    const body = workspace.getByRole('textbox', { name: '编辑设定：港口船长', exact: true })
    await body.fill('Saved before opening the library.')
    await directory.getByRole('button', { name: '资料总览', exact: true }).click()
    await expect(library.getByTestId('lore-card-hero')).toContainText('港口船长')
    expect((await items()).find(item => item.id === 'hero')).toMatchObject({ name: '港口船长', content: expect.stringContaining('Saved before opening') })
    await library.getByTestId('lore-card-hero').getByRole('button', { name: '港口船长', exact: true }).click()
    await workspace.getByRole('button', { name: /资料属性/ }).click()
    const tags = workspace.getByTestId('lore-tags-input')
    const input = tags.getByLabel('标签', { exact: true })
    const badge = tags.getByText('主角', { exact: true })
    const inputBox = (await input.boundingBox())!
    const badgeBox = (await badge.boundingBox())!
    expect(Math.abs(inputBox.y + inputBox.height / 2 - badgeBox.y - badgeBox.height / 2)).toBeLessThan(3)
    await input.fill('船员')
    await input.press('Enter')
    await expect(input).toBeFocused()
    await input.fill(longTag)
    await input.press('Enter')
    await tags.getByRole('button', { name: '移除标签：船员', exact: true }).click()
    await page.screenshot({ path: testInfo.outputPath(`workspace-${theme}-wide.png`) })

    await page.setViewportSize({ width: 390, height: 844 })
    await tags.scrollIntoViewIfNeeded()
    expect(await tags.evaluate(node => node.scrollWidth <= node.clientWidth)).toBe(true)
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
    await page.screenshot({ path: testInfo.outputPath(`workspace-${theme}-narrow.png`) })
    await page.setViewportSize({ width: 1920, height: 1080 })
    await directory.getByRole('button', { name: '资料索引', exact: true }).click()
    const indexEditor = workspace.getByTestId('lore-index-editor')
    await indexEditor.getByRole('button', { name: '阅读指引', exact: true }).click()
    await indexEditor.getByRole('textbox', { name: '阅读指引', exact: true }).fill('Read the harbor lore first.')
    await directory.getByRole('button', { name: '资料总览', exact: true }).click()
    // Navigation finishes only after the draft (including conflict recovery) is saved.
    await expect(library).toBeVisible()
    await expect(indexEditor).toBeHidden()
    expect((await index()).intro_markdown).toContain('Read the harbor lore first.')
    await directory.getByRole('button', { name: '资料索引', exact: true }).click()
    await expect(indexEditor.getByRole('textbox', { name: '阅读指引', exact: true })).toContainText('Read the harbor lore first.')
    await page.setViewportSize({ width: 390, height: 844 })
    expect(await indexEditor.evaluate(node => node.scrollWidth <= node.clientWidth)).toBe(true)
    await workspace.getByRole('button', { name: '打开设定目录', exact: true }).click()
    await page.getByRole('dialog', { name: '设定目录', exact: true }).getByRole('button', { name: '资料总览', exact: true }).click()
    await expect(library.getByTestId('lore-card-hero')).toBeVisible()
    await page.screenshot({ path: testInfo.outputPath(`workspace-${theme}-narrow-overview.png`) })
    await library.getByTestId('lore-card-port').getByRole('button', { name: longName, exact: true }).click()
    await expect(workspace.getByRole('button', { name: `修改名称：${longName}`, exact: true })).toBeVisible()
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)

    await page.setViewportSize({ width: 1920, height: 1080 })
    await sidebar.getByRole('button', { name: '游戏', exact: true }).click()
    await sidebar.getByRole('button', { name: '资料库', exact: true }).click()
    await page.getByRole('button', { name: '资料索引', exact: true }).click()
    await expect(page.getByTestId('lore-index-editor').getByRole('textbox', { name: '阅读指引', exact: true })).toContainText('Read the harbor lore first.')
    expect((await items()).find(item => item.id === 'hero')?.tags).toEqual(['东陵', '主角', longTag])
    if (theme === 'light') {
      const current = await (await request.get('/api/settings')).json()
      await request.patch('/api/settings', { data: { layer: 'user', base_revision: current.revisions.user, changes: { language: 'en-US' } } })
      await page.reload()
      await page.getByLabel('Workbench sidebar').getByRole('button', { name: 'Writing', exact: true }).click()
      await expect(page.getByRole('region', { name: 'Story Lore', exact: true }).getByRole('button', { name: `Rename: ${longName}`, exact: true })).toBeVisible()
    }
  })
}

test('keeps the lore index open until a conflicted save finishes', async ({ page, request }) => {
  const book = await createAndOpenBook(request, 'Lore index save recovery')
  const indexPath = `/api/projects/${book.projectId}/book/lore/index`
  const snapshot = async () => (await (await request.get(indexPath)).json())
  await page.setViewportSize({ width: 1920, height: 1080 })
  await page.goto('/')
  await page.getByLabel('工作台侧边栏').getByRole('button', { name: '写作', exact: true }).click()
  await page.getByTestId('book-settings-header-frame').getByRole('button', { name: '设定', exact: true }).click()
  const workspace = page.getByRole('region', { name: '作品设定', exact: true })
  const directory = workspace.locator('[data-slot="sidebar"]')
  await directory.getByRole('button', { name: '资料索引', exact: true }).click()
  const editor = workspace.getByTestId('lore-index-editor')
  const library = workspace.getByTestId('lore-library')
  await expect(editor).toBeVisible()

  let releaseSave!: () => void
  const saveGate = new Promise<void>(resolve => { releaseSave = resolve })
  let writes = 0
  await page.route(`**${indexPath}`, async route => {
    if (route.request().method() !== 'PUT') { await route.continue(); return }
    writes += 1
    if (writes === 1) {
      // Advance the real file revision immediately before the captured write.
      // This guarantees a conflict independently of file-watch/refetch timing.
      const latest = await snapshot()
      const external = await request.put(indexPath, { data: {
        base_revision: latest.revision,
        guide: { ...latest.guide, automatic_details: { resident: 'brief' } },
      } })
      expect(external.ok(), await external.text()).toBe(true)
      const conflict = await route.fetch()
      expect(conflict.status()).toBe(409)
      expect((await conflict.json()).code).toBe('api.resource.revisionConflict')
      await route.fulfill({ response: conflict })
      return
    }
    await saveGate
    await route.continue()
  })
  try {
    await editor.getByRole('button', { name: '阅读指引', exact: true }).click()
    await editor.getByRole('textbox', { name: '阅读指引', exact: true }).fill('Saved after conflict recovery.')
    await directory.getByRole('button', { name: '资料总览', exact: true }).click()
    await expect.poll(() => writes).toBe(2)
    await expect(editor).toBeVisible()
    await expect(library).toBeHidden()
    expect((await snapshot()).guide).toMatchObject({ intro_markdown: '', automatic_details: { resident: 'brief' } })
  } finally {
    releaseSave()
  }
  await expect(library).toBeVisible()
  await expect(editor).toBeHidden()
  expect((await snapshot()).guide).toMatchObject({
    intro_markdown: expect.stringContaining('Saved after conflict recovery.'), automatic_details: { resident: 'brief' },
  })
  await page.reload()
  await directory.getByRole('button', { name: '资料索引', exact: true }).click()
  await expect(editor.getByRole('textbox', { name: '阅读指引', exact: true })).toContainText('Saved after conflict recovery.')
})
