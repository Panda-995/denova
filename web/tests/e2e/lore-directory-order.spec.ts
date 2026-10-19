import { expect, test, type Locator, type Page } from '../support/fixtures'
import { createAndOpenBook } from '../support/api'

async function drag(page: Page, source: Locator, target: Locator, screenshotPath?: string) {
  await source.scrollIntoViewIfNeeded()
  await source.hover()
  await expect(source.locator('..')).toHaveCSS('transform', 'none')
  await expect(target.locator('..')).toHaveCSS('transform', 'none')
  const from = (await source.boundingBox())!
  const to = (await target.boundingBox())!
  await page.mouse.move(from.x + from.width / 2, from.y + from.height / 2)
  await page.mouse.down()
  await page.mouse.move(from.x + from.width / 2 + 12, from.y + from.height / 2, { steps: 3 })
  await expect(source).toHaveAttribute('aria-pressed', 'true')
  await expect(source).toHaveCSS('opacity', '1')
  await expect(page.locator('[data-resource-directory-drag-overlay]')).toHaveCount(0)
  await page.mouse.move(to.x + to.width / 2, to.y + to.height / 2, { steps: 15 })
  const moving = (await source.boundingBox())!
  expect(Math.abs(moving.x - from.x)).toBeLessThan(2)
  expect(Math.abs(moving.y - to.y)).toBeLessThan(2)
  expect(Math.abs(moving.width - from.width)).toBeLessThan(2)
  // Geometry alone does not detect rows clipped by their paint-containing slot.
  for (const row of [source, target]) {
    await expect.poll(() => row.evaluate(node => {
      const rect = node.getBoundingClientRect()
      return node.contains(document.elementFromPoint(rect.x + rect.width / 2, rect.y + rect.height / 2))
    })).toBe(true)
  }
  if (screenshotPath) await page.screenshot({ path: screenshotPath })
  await page.mouse.up()
  // Check the first frames after drop, not just the eventual DOM order.
  const droppedPositions = await source.evaluate(async node => {
    const positions: number[] = []
    for (let frame = 0; frame < 3; frame++) {
      await new Promise<void>(resolve => requestAnimationFrame(() => resolve()))
      positions.push(node.getBoundingClientRect().y)
    }
    return positions
  })
  for (const position of droppedPositions) expect(Math.abs(position - to.y)).toBeLessThan(2)
}

for (const theme of ['dark', 'light']) {
  test(theme === 'light' ? 'renders long Lore directory names at wide and narrow widths in light' : 'shares direct Lore directory sorting across surfaces and keeps hidden entries in dark', async ({ page, request }, testInfo) => {
    const book = await createAndOpenBook(request, `Lore directory ${theme}`)
    const url = `/api/projects/${book.projectId}/book/lore/items`
    const gamma = 'Gamma 很长的资料名称LongUnbrokenName'.repeat(4)
    for (const [id, name, tags] of [['alpha', 'Alpha', ['保留']], ['beta', 'Beta', []], ['gamma', gamma, ['保留']]] as const) {
      const created = await request.post(url, { data: { id, name, tags, type: 'character', content: `Body of ${id}`, brief_description: 'Long summary '.repeat(30) } })
      expect(created.ok(), await created.text()).toBe(true)
    }
    const before = (await (await request.get(url)).json()).items
    const settings = await (await request.get('/api/settings')).json()
    await request.patch('/api/settings', { data: { layer: 'user', base_revision: settings.revisions.user, changes: { theme, language: 'zh-CN' } } })
    await page.setViewportSize({ width: 1680, height: 1000 })
    await page.goto('/')
    const sidebar = page.getByLabel('工作台侧边栏')
    await sidebar.getByRole('button', { name: '资料库', exact: true }).click()
    const library = page.getByTestId('lore-library')
    const directory = page.locator('.nova-embedded-sidebar').filter({ has: page.getByRole('textbox', { name: '搜索资料', exact: true }) })
    const rows = directory.locator('[data-slot="sidebar-menu-button"][title]')
    const row = (name: string) => rows.filter({ has: page.getByText(name, { exact: true }) })
    const names = () => rows.evaluateAll(nodes => nodes.map(node => node.querySelector('span.min-w-0')?.firstElementChild?.textContent))
    await expect.poll(names).toEqual(['Alpha', 'Beta', gamma])
    await expect(row(gamma)).toHaveCSS('cursor', 'default')
    await expect(row(gamma)).toHaveAttribute('title', gamma)
    await drag(page, row(gamma), row('Alpha'), testInfo.outputPath(`directory-${theme}-dragging.png`))
    await expect.poll(names).toEqual([gamma, 'Alpha', 'Beta'])
    await expect(library).toBeVisible()
    // Sorting persistence and Project isolation run once. Keep long-name
    // directory rendering in both themes, including the mobile drawer.
    if (theme === 'light') {
      await page.screenshot({ path: testInfo.outputPath(`directory-${theme}-wide.png`) })
      await page.setViewportSize({ width: 390, height: 844 })
      await page.getByRole('button', { name: '资料库目录', exact: true }).click()
      await expect(row(gamma)).toBeVisible()
      expect(await directory.evaluate(node => node.scrollWidth <= node.clientWidth)).toBe(true)
      await page.screenshot({ path: testInfo.outputPath(`directory-${theme}-narrow.png`) })
      return
    }
    await page.reload()
    await expect.poll(names).toEqual([gamma, 'Alpha', 'Beta'])
    // Space sorts; Enter still opens a resource. Escape cancels without saving.
    await row('Beta').press('Space')
    await expect(row('Beta')).toHaveAttribute('aria-pressed', 'true')
    await row('Beta').press('ArrowUp')
    await expect(directory.getByRole('status')).toHaveText('移动到第 2 项，共 3 项。')
    await row('Beta').press('Space')
    await expect.poll(names).toEqual([gamma, 'Beta', 'Alpha'])
    await row('Alpha').press('Space')
    await expect(row('Alpha')).toHaveAttribute('aria-pressed', 'true')
    await row('Alpha').press('ArrowUp')
    await row('Alpha').press('Escape')
    await expect.poll(names).toEqual([gamma, 'Beta', 'Alpha'])
    await directory.getByRole('button', { name: '筛选资料', exact: true }).click()
    const filters = page.getByRole('dialog', { name: '资料筛选', exact: true })
    await filters.getByRole('checkbox', { name: /^保留/ }).check()
    await filters.press('Escape')
    await expect(filters).toBeHidden()
    await expect.poll(names).toEqual([gamma, 'Alpha'])
    await drag(page, row('Alpha'), row(gamma))
    await expect.poll(names).toEqual(['Alpha', gamma])
    await directory.getByRole('button', { name: '清除筛选', exact: true }).click()
    await expect.poll(names).toEqual(['Alpha', 'Beta', gamma])
    await row('Alpha').press('Enter')
    await expect(page.getByLabel('名称', { exact: true })).toHaveValue('Alpha')
    await page.getByRole('button', { name: '返回资料总览', exact: true }).click()
    await page.screenshot({ path: testInfo.outputPath(`directory-${theme}-wide.png`) })

    await sidebar.getByRole('button', { name: '写作', exact: true }).click()
    await page.getByTestId('book-settings-header-frame').getByRole('button', { name: '设定', exact: true }).click()
    const workspace = page.getByRole('region', { name: '作品设定', exact: true })
    const writingRows = workspace.locator('[data-slot="sidebar-menu-button"][title]')
    await expect.poll(() => writingRows.evaluateAll(nodes => nodes.map(node => node.querySelector('span.min-w-0')?.firstElementChild?.textContent))).toEqual(['Alpha', 'Beta', gamma])
    await drag(page, writingRows.filter({ has: page.getByText('Beta', { exact: true }) }), writingRows.filter({ has: page.getByText('Alpha', { exact: true }) }), testInfo.outputPath(`directory-${theme}-dragging.png`))
    await expect.poll(() => writingRows.evaluateAll(nodes => nodes.map(node => node.querySelector('span.min-w-0')?.firstElementChild?.textContent))).toEqual(['Beta', 'Alpha', gamma])
    for (const surface of ['写作', '游戏']) {
      await sidebar.getByRole('button', { name: surface, exact: true }).click()
      await sidebar.getByRole('button', { name: '资料库', exact: true }).click()
      await expect.poll(names).toEqual(['Beta', 'Alpha', gamma])
    }
    await page.setViewportSize({ width: 390, height: 844 })
    await page.getByRole('button', { name: '资料库目录', exact: true }).click()
    await expect(row(gamma)).toBeVisible()
    expect(await directory.evaluate(node => node.scrollWidth <= node.clientWidth)).toBe(true)
    await page.screenshot({ path: testInfo.outputPath(`directory-${theme}-narrow.png`) })
    expect((await (await request.get(url)).json()).items).toEqual(before)
    // Identical resource IDs in another Project must not inherit this order.
    const next = await createAndOpenBook(request, `Other directory ${theme}`)
    const nextURL = `/api/projects/${next.projectId}/book/lore/items`
    for (const id of ['alpha', 'beta']) expect((await request.post(nextURL, { data: { id, name: id === 'alpha' ? 'Alpha' : 'Beta', type: 'character' } })).ok()).toBe(true)
    await page.setViewportSize({ width: 1680, height: 1000 })
    await page.reload()
    await expect.poll(names).toEqual(['Alpha', 'Beta'])
  })
}

test('sorts a scrolling directory using only the original row', async ({ page, request }) => {
  const book = await createAndOpenBook(request, 'Scrolling directory')
  const url = `/api/projects/${book.projectId}/book/lore/items`
  const names = Array.from({ length: 24 }, (_, index) => `Entry ${String(index).padStart(2, '0')}`)
  for (const name of names) expect((await request.post(url, { data: { id: name.replace(' ', '-'), name, type: 'character' } })).ok()).toBe(true)
  await page.setViewportSize({ width: 1280, height: 600 })
  await page.goto('/')
  await page.getByLabel('工作台侧边栏').getByRole('button', { name: '资料库', exact: true }).click()
  const directory = page.locator('.nova-embedded-sidebar').filter({ has: page.getByRole('textbox', { name: '搜索资料', exact: true }) })
  const rows = directory.locator('[data-slot="sidebar-menu-button"][title]')
  const source = rows.filter({ has: page.getByText(names[23], { exact: true }) })
  const target = rows.filter({ has: page.getByText(names[0], { exact: true }) })
  const scrollable = directory.locator('[data-slot="sidebar-content"]')
  await source.scrollIntoViewIfNeeded()
  await source.hover()
  const from = (await source.boundingBox())!
  const container = (await scrollable.boundingBox())!
  const x = from.x + from.width / 2
  await page.mouse.move(x, from.y + from.height / 2)
  await page.mouse.down()
  await page.mouse.move(x + 12, from.y + from.height / 2, { steps: 3 })
  await expect(source).toHaveAttribute('aria-pressed', 'true')
  await page.mouse.move(x, container.y + 10, { steps: 15 })
  await expect.poll(() => scrollable.evaluate(node => node.scrollTop)).toBeLessThan(1)
  // The first row has moved aside during sorting; drop into its original slot.
  const to = await target.evaluate(node => {
    const rect = node.getBoundingClientRect()
    const transform = new DOMMatrixReadOnly(getComputedStyle(node.parentElement!).transform)
    return rect.y - transform.m42 + rect.height / 2
  })
  await page.mouse.move(x, to, { steps: 3 })
  await expect(source).toHaveCSS('opacity', '1')
  await expect(page.locator('[data-resource-directory-drag-overlay]')).toHaveCount(0)
  await page.mouse.up()
  await expect(rows.first()).toHaveAttribute('title', names[23])
  await page.reload()
  await expect(rows.first()).toHaveAttribute('title', names[23])
})
