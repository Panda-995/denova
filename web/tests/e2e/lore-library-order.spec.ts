import { expect, test, type Locator, type Page } from '../support/fixtures'
import { createAndOpenBook, createStartedStory } from '../support/api'

async function drag(page: Page, source: Locator, target: Locator, screenshotPath?: string) {
  await source.scrollIntoViewIfNeeded()
  await source.hover()
  const from = (await source.boundingBox())!
  const to = (await target.boundingBox())!
  await page.mouse.move(from.x + from.width / 2, from.y + from.height / 2)
  await page.mouse.down()
  await page.mouse.move(from.x + from.width / 2 + 12, from.y + from.height / 2, { steps: 3 })
  await page.mouse.move(to.x + to.width / 2, to.y + to.height / 2, { steps: 15 })
  if (screenshotPath) await page.screenshot({ path: screenshotPath })
  await page.mouse.up()
  await expect(source.locator('..')).not.toHaveAttribute('aria-pressed', 'true')
  // Wait for the drop transition before the next click or sorting operation.
  await expect(source.locator('..')).toHaveCSS('transform', 'none')
  // DndKit retains its document click suppression for 50 ms after pointer-up.
  await page.waitForTimeout(50)
}

for (const mode of ['writing', 'game'] as const) {
  test(`sorts overview cards and groups while preserving filtered entries in ${mode}`, async ({ page, request }, testInfo) => {
    const book = await createAndOpenBook(request, `Overview order ${mode}`)
    if (mode === 'game') await createStartedStory(request, 'Overview order game')
    const url = `/api/projects/${book.projectId}/book/lore/items`
    const gamma = 'Gamma 很长的资料名称LongUnbrokenName'.repeat(4)
    for (const [id, name, type, tags] of [
      ['alpha', 'Alpha', 'character', ['保留', '共同']],
      ['beta', 'Beta', 'character', ['共同']],
      ['gamma', gamma, 'character', ['保留', '共同']],
      ['world', 'World', 'world', []],
    ] as const) {
      const created = await request.post(url, { data: { id, name, type, tags, content: `Body ${id}`, brief_description: 'Long summary '.repeat(30) } })
      expect(created.ok(), await created.text()).toBe(true)
    }
    const before = (await (await request.get(url)).json()).items
    const settings = await (await request.get('/api/settings')).json()
    await request.patch('/api/settings', { data: { layer: 'user', base_revision: settings.revisions.user, changes: { theme: mode === 'writing' ? 'dark' : 'light', language: 'zh-CN' } } })
    await page.setViewportSize({ width: 1680, height: 1000 })
    await page.goto('/')
    const sidebar = page.getByLabel('工作台侧边栏')
    await sidebar.getByRole('button', { name: mode === 'writing' ? '写作' : '游戏', exact: true }).click()
    await sidebar.getByRole('button', { name: '资料库', exact: true }).click()
    const library = page.getByTestId('lore-library')
    const characters = library.getByRole('region', { name: '角色', exact: true })
    const cards = (group: Locator) => group.locator('[data-slot="card-title"]')
    const trigger = (group: Locator) => group.getByRole('heading').getByRole('button')
    const sortable = (group: Locator, name: string) => group.getByRole('button', { name: `排序资料：${name}`, exact: true })
    const directory = page.locator('.nova-embedded-sidebar').filter({ has: page.getByRole('textbox', { name: '搜索资料', exact: true }) })
    const rows = directory.locator('[data-slot="sidebar-menu-button"][title]')
    const directoryNames = () => rows.evaluateAll(nodes => nodes.map(node => node.querySelector('span.min-w-0')?.firstElementChild?.textContent))
    await expect(cards(characters)).toHaveText(['Alpha', 'Beta', gamma])
    await drag(page, characters.getByTestId('lore-card-gamma'), characters.getByTestId('lore-card-alpha'), testInfo.outputPath(`overview-${mode}-dragging.png`))
    await expect(cards(characters)).toHaveText([gamma, 'Alpha', 'Beta'])
    await expect.poll(directoryNames).toEqual([gamma, 'Alpha', 'Beta', 'World'])
    await expect(library).toBeVisible()
    await page.reload()
    await expect(cards(characters)).toHaveText([gamma, 'Alpha', 'Beta'])
    await sortable(characters, 'Beta').press('Space')
    await expect(sortable(characters, 'Beta')).toHaveAttribute('aria-pressed', 'true')
    await sortable(characters, 'Beta').press('ArrowLeft')
    await expect(characters.getByRole('status')).toHaveText('移动到第 2 项，共 3 项。')
    await sortable(characters, 'Beta').press('Space')
    await expect(cards(characters)).toHaveText([gamma, 'Beta', 'Alpha'])
    await sortable(characters, 'Alpha').press('Space')
    await expect(sortable(characters, 'Alpha')).toHaveAttribute('aria-pressed', 'true')
    await sortable(characters, 'Alpha').press('ArrowLeft')
    await sortable(characters, 'Alpha').press('Escape')
    await expect(cards(characters)).toHaveText([gamma, 'Beta', 'Alpha'])

    await library.getByRole('button', { name: '筛选资料', exact: true }).click()
    const filters = page.getByRole('dialog', { name: '资料筛选', exact: true })
    await filters.getByRole('checkbox', { name: /^保留/ }).check()
    await filters.press('Escape')
    await expect(cards(characters)).toHaveText([gamma, 'Alpha'])
    await drag(page, characters.getByTestId('lore-card-alpha'), characters.getByTestId('lore-card-gamma'))
    await library.getByRole('button', { name: '清除筛选', exact: true }).click()
    await expect(cards(characters)).toHaveText(['Alpha', 'Beta', gamma])
    await expect.poll(directoryNames).toEqual(['Alpha', 'Beta', gamma, 'World'])

    await library.getByRole('button', { name: '批量操作', exact: true }).click()
    await characters.getByRole('checkbox', { name: '选择资料：Alpha', exact: true }).check()
    await expect(characters.getByTestId('lore-card-alpha').locator('..')).not.toHaveAttribute('tabindex')
    await expect(characters.getByRole('checkbox', { name: '选择资料：Alpha', exact: true })).toBeChecked()
    await library.getByRole('button', { name: '退出多选', exact: true }).click()
    // Cover controls handle pointer movement without starting a card drag.
    const upload = characters.getByTestId('lore-card-alpha').getByRole('button', { name: '上传封面', exact: true })
    const control = (await upload.boundingBox())!
    await page.mouse.move(control.x + control.width / 2, control.y + control.height / 2)
    await page.mouse.down()
    await page.mouse.move(control.x + control.width / 2 + 20, control.y + control.height / 2)
    await expect(sortable(characters, 'Alpha')).not.toHaveAttribute('aria-pressed', 'true')
    await page.keyboard.press('Escape')
    await page.mouse.up()

    await library.getByRole('button', { name: '收起全部', exact: true }).click()
    const world = library.getByRole('region', { name: '世界设定', exact: true })
    await drag(page, trigger(world), trigger(characters))
    await expect.poll(() => library.getByRole('region').evaluateAll(nodes => nodes.map(node => node.getAttribute('aria-label')))).toEqual(['世界设定', '角色'])
    await page.reload()
    await expect.poll(() => library.getByRole('region').evaluateAll(nodes => nodes.map(node => node.getAttribute('aria-label')))).toEqual(['世界设定', '角色'])

    await library.getByRole('radio', { name: '标签视图', exact: true }).click()
    const kept = library.getByRole('region', { name: '保留', exact: true })
    const shared = library.getByRole('region', { name: '共同', exact: true })
    await drag(page, kept.getByTestId('lore-card-gamma'), kept.getByTestId('lore-card-alpha'))
    await expect(cards(kept)).toHaveText([gamma, 'Alpha'])
    await expect(cards(shared)).toHaveText(['Alpha', 'Beta', gamma])
    await library.getByRole('button', { name: '收起全部', exact: true }).click()
    await drag(page, trigger(shared), trigger(kept))
    await expect(library.getByRole('region').first()).toHaveAttribute('aria-label', '共同')
    await library.getByRole('radio', { name: '分类视图', exact: true }).click()
    for (const surface of ['写作', '游戏']) {
      await sidebar.getByRole('button', { name: surface, exact: true }).click()
      await sidebar.getByRole('button', { name: '资料库', exact: true }).click()
      await expect(cards(characters)).toHaveText(['Alpha', 'Beta', gamma])
    }
    await page.screenshot({ path: testInfo.outputPath(`overview-${mode}-wide.png`) })
    await page.setViewportSize({ width: 390, height: 844 })
    expect(await library.evaluate(node => node.scrollWidth <= node.clientWidth)).toBe(true)
    await page.screenshot({ path: testInfo.outputPath(`overview-${mode}-narrow.png`) })
    await characters.getByRole('button', { name: 'Alpha', exact: true }).click()
    await expect(page.getByLabel('名称', { exact: true })).toHaveValue('Alpha')
    expect((await (await request.get(url)).json()).items).toEqual(before)
  })
}
