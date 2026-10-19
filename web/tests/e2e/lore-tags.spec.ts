import { expect, test } from '../support/fixtures'
import { createAndOpenBook } from '../support/api'
import type { LoreItem } from '../../src/lib/api'

for (const theme of ['dark', 'light']) {
  test(`manages tagged lore across library and writing in ${theme}`, async ({ page, request }, testInfo) => {
    const book = await createAndOpenBook(request, `Lore tags ${theme}`)
    const base = `/api/projects/${book.projectId}/book/lore`
    const entries = async (): Promise<LoreItem[]> => (await (await request.get(`${base}/items`)).json()).items
    const settings = await (await request.get('/api/settings')).json()
    await request.patch('/api/settings', { data: { layer: 'user', base_revision: settings.revisions.user, changes: { theme, language: 'zh-CN' } } })
    await page.setViewportSize({ width: 1680, height: 1000 })
    await page.goto('/')
    const sidebar = page.getByLabel('工作台侧边栏')
    await sidebar.getByRole('button', { name: '资料库', exact: true }).click()
    const library = page.getByTestId('lore-library')
    await library.getByRole('radio', { name: '标签视图', exact: true }).click()
    await expect(library.getByText('还没有资料', { exact: true })).toBeVisible()
    await page.screenshot({ path: testInfo.outputPath(`tags-${theme}-empty.png`) })

    const longTag = 'LongUnbrokenTag'.repeat(10)
    for (const [id, name, type, tags] of [
      ['hero', '港口人物', 'character', ['东陵', 'protagonist', '主角']],
      ['port', '旧港口', 'location', ['东陵', longTag]],
      ['note', '未分类笔记', 'world', []],
    ] as const) {
      const response = await request.post(`${base}/items`, { data: { id, name, type, tags, content: `Body of ${id}` } })
      expect(response.ok(), await response.text()).toBe(true)
    }
    const snapshot = await (await request.get(`${base}/index`)).json()
    const longGroup = 'GroupWithAVeryLongName'.repeat(5)
    const updated = await request.put(`${base}/index`, { data: { base_revision: snapshot.revision, guide: {
      ...snapshot.guide, groups: [
        { id: 'harbor', name: '港口设定', default_detail: 'brief', purpose: '', body_markdown: '' },
        { id: 'long', name: longGroup, default_detail: 'name', purpose: '', body_markdown: '' },
      ],
    } } })
    expect(updated.ok(), await updated.text()).toBe(true)
    await expect(library.getByLabel('当前筛选条件')).toContainText('3 / 3 项')
    const east = library.getByRole('region', { name: '东陵', exact: true })
    await expect(east.getByTestId('lore-card-hero')).toBeVisible()
    await expect(east.getByTestId('lore-card-port')).toBeVisible()
    await expect(library.getByRole('region', { name: '主角', exact: true }).getByTestId('lore-card-hero')).toHaveCount(1)
    await expect(library.getByRole('region', { name: '未打标签', exact: true }).getByTestId('lore-card-note')).toBeVisible()
    await east.getByRole('button', { name: '东陵 2', exact: true }).click()
    await expect(east.getByTestId('lore-card-hero')).toBeHidden()
    await east.getByRole('button', { name: '东陵 2', exact: true }).click()
    await library.getByRole('button', { name: '批量操作', exact: true }).click()
    await library.getByRole('button', { name: '全选当前结果', exact: true }).click()
    await expect(library.getByRole('toolbar', { name: '批量操作', exact: true }).getByRole('status')).toHaveText('已选 3 项')
    await east.getByTestId('lore-card-hero').getByRole('checkbox').uncheck()
    await expect(library.getByRole('toolbar', { name: '批量操作', exact: true }).getByRole('status')).toHaveText('已选 2 项')
    await expect(library.getByRole('region', { name: '主角', exact: true }).getByRole('checkbox')).not.toBeChecked()
    await library.getByRole('button', { name: '退出多选', exact: true }).click()
    await page.screenshot({ path: testInfo.outputPath(`tags-${theme}-wide.png`) })
    await page.setViewportSize({ width: 390, height: 844 })
    await library.getByRole('region', { name: longTag, exact: true }).scrollIntoViewIfNeeded()
    expect(await library.evaluate(node => node.scrollWidth <= node.clientWidth)).toBe(true)
    await page.screenshot({ path: testInfo.outputPath(`tags-${theme}-narrow.png`) })
    await page.setViewportSize({ width: 1680, height: 1000 })
    await east.getByRole('button', { name: '港口人物', exact: true }).click()
    const memberships = page.getByTestId('lore-index-memberships')
    await expect(memberships).toBeVisible()
    const groupPicker = page.getByRole('dialog', { name: '所属分组', exact: true })
    await memberships.getByRole('button', { name: '所属分组', exact: true }).click()
    await groupPicker.getByRole('checkbox', { name: '港口设定', exact: true }).check()
    const membershipHeight = (await memberships.boundingBox())!.height
    await groupPicker.getByRole('checkbox', { name: longGroup, exact: true }).check()
    await expect.poll(async () => (await memberships.boundingBox())!.height).toBe(membershipHeight)
    await page.setViewportSize({ width: 390, height: 844 })
    expect(await groupPicker.evaluate(node => node.scrollWidth <= node.clientWidth)).toBe(true)
    await page.screenshot({ path: testInfo.outputPath(`groups-${theme}-narrow.png`) })
    await groupPicker.getByRole('checkbox', { name: longGroup, exact: true }).uncheck()
    await groupPicker.press('Escape')
    await page.setViewportSize({ width: 1680, height: 1000 })
    await expect.poll(async () => (await entries()).find(item => item.id === 'hero')?.index_memberships).toEqual([{ group_id: 'harbor', detail: 'inherit' }])

    const tags = page.getByTestId('lore-tags-input')
    const input = tags.getByLabel('标签', { exact: true })
    await input.fill('同伴')
    await input.press('Enter')
    await input.fill('同伴，航海士,东陵')
    await tags.getByRole('button', { name: '添加标签', exact: true }).click()
    await tags.getByRole('button', { name: '移除标签：航海士', exact: true }).click()
    await input.fill('中文输入')
    await input.dispatchEvent('compositionstart')
    await input.press('Enter')
    await expect(input).toHaveValue('中文输入')
    await input.dispatchEvent('compositionend')
    await input.press('Enter')
    await input.fill(longTag)
    await input.press('Tab')
    const expected = ['东陵', 'protagonist', '主角', '同伴', '中文输入', longTag]
    await expect.poll(async () => (await entries()).find(item => item.id === 'hero')?.tags).toEqual(expected)
    await page.screenshot({ path: testInfo.outputPath(`tag-editor-${theme}-wide.png`) })
    await page.setViewportSize({ width: 390, height: 844 })
    await tags.scrollIntoViewIfNeeded()
    expect(await tags.evaluate(node => node.scrollWidth <= node.clientWidth)).toBe(true)
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
    await page.screenshot({ path: testInfo.outputPath(`tag-editor-${theme}-narrow.png`) })

    await page.setViewportSize({ width: 1920, height: 1080 })
    await sidebar.getByRole('button', { name: '写作', exact: true }).click()
    await page.getByTestId('book-settings-header-frame').getByRole('button', { name: '设定', exact: true }).click()
    const workspace = page.getByRole('region', { name: '作品设定', exact: true })
    await workspace.getByRole('button', { name: /^港口人物/ }).click()
    await workspace.getByRole('button', { name: /资料属性/ }).click()
    await expect(workspace.getByTestId('lore-index-memberships')).toContainText('港口设定')
    await expect(workspace.getByRole('combobox', { name: '加载策略', exact: true })).toHaveCount(1)
    const writingTags = workspace.getByTestId('lore-tags-input')
    await writingTags.getByRole('button', { name: '移除标签：同伴', exact: true }).click()
    await writingTags.getByLabel('标签', { exact: true }).fill('写作标签')
    await writingTags.getByLabel('标签', { exact: true }).press('Enter')
    await expect.poll(async () => (await entries()).find(item => item.id === 'hero')?.tags).toEqual([...expected.filter(tag => tag !== '同伴'), '写作标签'])
    await memberships.getByRole('button', { name: '所属分组', exact: true }).click()
    await groupPicker.getByRole('checkbox', { name: '港口设定', exact: true }).uncheck()
    await groupPicker.press('Escape')
    await expect.poll(async () => (await entries()).find(item => item.id === 'hero')?.index_memberships ?? []).toEqual([])
    await page.screenshot({ path: testInfo.outputPath(`tag-writing-${theme}.png`) })
    await sidebar.getByRole('button', { name: '游戏', exact: true }).click()
    await sidebar.getByRole('button', { name: '资料库', exact: true }).click()
    await page.getByRole('button', { name: '返回资料总览', exact: true }).click()
    await library.getByRole('radio', { name: '标签视图', exact: true }).click()
    await expect(library.getByRole('region', { name: '写作标签', exact: true }).getByTestId('lore-card-hero')).toBeVisible()
    await page.reload()
    await library.getByRole('radio', { name: '标签视图', exact: true }).click()
    await expect(library.getByRole('region', { name: '写作标签', exact: true }).getByTestId('lore-card-hero')).toBeVisible()
    if (theme === 'light') {
      const current = await (await request.get('/api/settings')).json()
      await request.patch('/api/settings', { data: { layer: 'user', base_revision: current.revisions.user, changes: { language: 'en-US' } } })
      await page.reload()
      await library.getByRole('radio', { name: 'Tag view', exact: true }).click()
      await library.getByRole('region', { name: '写作标签', exact: true }).getByTestId('lore-card-hero').click()
      await expect(page.getByTestId('lore-index-memberships').getByRole('button', { name: 'Group membership', exact: true })).toContainText('Automatic groups')
      await expect(page.getByPlaceholder('Type a tag and press Enter')).toBeVisible()
      await page.screenshot({ path: testInfo.outputPath('tag-editor-english.png') })
    }
  })
}
