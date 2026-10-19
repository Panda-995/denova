import { readFile, readdir, writeFile } from 'node:fs/promises'
import path from 'node:path'
import { test, expect } from '../support/fixtures'
import { createAndOpenBook } from '../support/api'

for (const theme of ['dark', 'light']) {
  test(theme === 'light' ? 'renders empty Lore categories at wide and narrow widths in light' : 'migrates and manages project categories across Writing and Game in dark', async ({ page, request }, testInfo) => {
    const book = await createAndOpenBook(request, `Categories ${theme}`)
    const base = `/api/projects/${book.projectId}/book/lore`
    const settings = await (await request.get('/api/settings')).json()
    await request.patch('/api/settings', { data: { layer: 'user', base_revision: settings.revisions.user, changes: { theme, language: 'zh-CN' } } })
    await page.goto('/')
    const sidebar = page.getByLabel('工作台侧边栏')
    await sidebar.getByRole('button', { name: '资料库', exact: true }).click()
    const library = page.getByTestId('lore-library')
    await expect(library.getByText('还没有资料', { exact: true })).toBeVisible()
    await page.screenshot({ path: testInfo.outputPath(`categories-${theme}-empty.png`) })
    // Migration and category identity do not depend on visual theme.
    if (theme === 'light') {
      await page.setViewportSize({ width: 390, height: 844 })
      await expect(library.getByText('还没有资料', { exact: true })).toBeVisible()
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
      await page.screenshot({ path: testInfo.outputPath(`categories-${theme}-empty-narrow.png`) })
      return
    }
    const legacy = JSON.stringify({ version: 2, items: [
      { id: 'hero', type: 'character', name: 'Hero', content: 'Identity', tags: ['主角'] },
      { id: 'guild', type: 'faction', name: 'Guild', content: 'Organization' },
      { id: 'rule', type: 'rule', name: 'Rule', content: 'Canon' },
      { id: 'template', type: 'other', name: 'Template', content: 'Keep this text', tags: ['模板'] },
    ] })
    // This workspace is owned by the isolated test backend, never the dev service.
    const lorePath = path.join(book.workspace, 'setting/lore/items.json')
    await writeFile(lorePath, legacy)
    await page.reload()
    await expect(library.getByRole('region', { name: '世界设定', exact: true })).toBeVisible()
    await expect(library.getByRole('region', { name: '组织', exact: true })).toBeVisible()
    await library.getByRole('button', { name: '管理分类', exact: true }).click()
    let manager = page.getByRole('dialog', { name: '管理分类', exact: true })
    await expect(manager.getByLabel('分类名称：角色', { exact: true })).toBeDisabled()
    await manager.getByLabel('新分类名称', { exact: true }).fill('技艺')
    await manager.getByRole('button', { name: '添加', exact: true }).click()
    await expect(manager.getByLabel('分类名称：技艺', { exact: true })).toBeVisible()
    const categories = await (await request.get(`${base}/categories`)).json()
    const custom = categories.find((c: { name: string }) => c.name === '技艺')
    expect(custom?.id).toBeTruthy()
    await manager.getByRole('button', { name: '关闭', exact: true }).first().click()
    await expect(manager).toBeHidden()
    await library.getByRole('button', { name: '新建资料', exact: true }).click()
    await page.getByRole('menuitem', { name: '技艺', exact: true }).click()
    await page.getByLabel('名称', { exact: true }).fill('Flight')
    const editor = page.getByRole('textbox', { name: '正文', exact: true })
    await editor.click()
    await expect(editor).toHaveAttribute('contenteditable', 'true')
    await expect(editor).toBeFocused()
    await page.keyboard.press(process.platform === 'darwin' ? 'Meta+A' : 'Control+A')
    await page.keyboard.insertText('Stable ability')
    await page.keyboard.press(process.platform === 'darwin' ? 'Meta+S' : 'Control+S')
    await expect.poll(async () => (await (await request.get(`${base}/items`)).json()).items.find((item: { name: string }) => item.name === 'Flight')).toMatchObject({ type: custom.id, content: 'Stable ability' })
    await expect(page.getByRole('status', { name: '所有更改均已保存' })).toBeVisible()
    await page.getByRole('button', { name: '返回资料总览', exact: true }).click()
    await expect(library).toBeVisible()
    await library.getByRole('button', { name: '管理分类', exact: true }).click()
    const longName = '技艺与能力的很长分类名称'.repeat(3)
    await manager.getByLabel('分类名称：技艺', { exact: true }).fill(longName)
    await manager.getByRole('button', { name: '保存名称：技艺', exact: true }).click()
    await expect(manager.getByLabel(`分类名称：${longName}`, { exact: true })).toBeVisible()
    await manager.getByRole('button', { name: `上移：${longName}`, exact: true }).click()
    await expect.poll(async () => (await (await request.get(`${base}/categories`)).json()).at(-2).id).toBe(custom.id)
    await page.setViewportSize({ width: 390, height: 844 })
    await page.screenshot({ path: testInfo.outputPath(`categories-${theme}-manager-narrow.png`) })
    expect(await manager.evaluate((node) => node.scrollWidth <= node.clientWidth)).toBe(true)
    await manager.getByRole('button', { name: '关闭', exact: true }).first().click()
    await expect(manager).toBeHidden()
    await library.getByRole('region', { name: longName, exact: true }).scrollIntoViewIfNeeded()
    await expect(library.getByRole('region', { name: longName, exact: true })).toBeVisible()
    await page.screenshot({ path: testInfo.outputPath(`categories-${theme}-library-narrow.png`) })
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
    await page.setViewportSize({ width: 1600, height: 1000 })
    for (const surface of ['写作', '游戏']) {
      await sidebar.getByRole('button', { name: surface, exact: true }).click()
      await sidebar.getByRole('button', { name: '资料库', exact: true }).click()
      await expect(library.getByRole('region', { name: longName, exact: true })).toBeVisible()
      await expect(library.getByRole('button', { name: 'Flight', exact: true })).toBeVisible()
    }
    await library.getByRole('button', { name: '管理分类', exact: true }).click()
    manager = page.getByRole('dialog', { name: '管理分类', exact: true })
    await manager.getByRole('button', { name: `删除分类：${longName}`, exact: true }).click()
    await manager.getByRole('combobox', { name: '资料移入', exact: true }).click()
    await page.getByRole('option', { name: '世界设定', exact: true }).click()
    await manager.getByRole('button', { name: '移动资料并删除分类', exact: true }).click()
    await expect(manager.getByLabel(`分类名称：${longName}`, { exact: true })).toHaveCount(0)
    await manager.getByRole('button', { name: '关闭', exact: true }).first().click()
    const entries = (await (await request.get(`${base}/items`)).json()).items
    expect(entries.find((item: { name: string }) => item.name === 'Flight')).toMatchObject({ type: 'world', content: expect.stringContaining('Stable ability') })
    expect(entries.find((item: { id: string }) => item.id === 'template')).toMatchObject({ type: 'world', content: 'Keep this text', tags: ['模板'] })
    const backupDir = path.join(book.workspace, 'setting/lore/backups')
    const backupFiles = await readdir(backupDir)
    expect(backupFiles.some((name) => name.startsWith('categories-removal-'))).toBe(true)
    expect(await readFile(path.join(backupDir, backupFiles.find((name) => name.startsWith('categories-migration-'))!), 'utf8')).toBe(legacy)
    const latest = await (await request.get('/api/settings')).json()
    await request.patch('/api/settings', { data: { layer: 'user', base_revision: latest.revisions.user, changes: { language: 'en-US' } } })
    await page.reload()
    await expect(library.getByRole('region', { name: 'Organization', exact: true })).toBeVisible()
    await page.screenshot({ path: testInfo.outputPath(`categories-${theme}-english-wide.png`) })
  })
}
