import { expect, test } from '../support/fixtures'
import { createAndOpenBook, createStartedStory } from '../support/api'
import type { Locator, Page } from '@playwright/test'

async function drag(page: Page, source: Locator, target: Locator) {
  await source.scrollIntoViewIfNeeded()
  await source.hover()
  const from = (await source.boundingBox())!
  const to = (await target.boundingBox())!
  await page.mouse.move(from.x + from.width / 2, from.y + from.height / 2)
  await page.mouse.down()
  await page.mouse.move(from.x + from.width / 2 + 12, from.y + from.height / 2, { steps: 3 })
  await page.mouse.move(to.x + to.width / 2, to.y + to.height / 2, { steps: 15 })
  await page.mouse.up()
}

for (const mode of ['writing', 'game'] as const) {
  test(`persists direct group and card dragging in the ${mode} lore index`, async ({ page, request }, testInfo) => {
    const book = await createAndOpenBook(request, `Index order ${mode}`)
    if (mode === 'game') await createStartedStory(request, 'Index order game')
    const base = `/api/projects/${book.projectId}/book/lore`
    const snapshot = async () => (await (await request.get(`${base}/index`)).json())
    const index = await snapshot()
    expect((await request.put(`${base}/index`, { data: { base_revision: index.revision, guide: {
      intro_markdown: '', groups: [
        { id: 'cast', name: '港口人物', purpose: '', body_markdown: '', default_detail: 'name' },
        { id: 'empty', name: '暂未关联资料的长标题LongGroupName'.repeat(4), purpose: '', body_markdown: '', default_detail: 'name' },
      ],
    } } })).ok()).toBe(true)
    for (const data of [
      { id: 'alpha', name: 'Alpha', type: 'world', importance: 'minor', index_memberships: [{ group_id: 'cast', detail: 'inherit' }] },
      { id: 'zulu', name: 'Zulu', type: 'character', importance: 'major', index_memberships: [{ group_id: 'cast', detail: 'inherit' }] },
      { id: 'auto-alpha', name: 'Auto Alpha', type: 'character' },
      { id: 'auto-zulu', name: 'Auto Zulu', type: 'character' },
      { id: 'law', name: 'World law', type: 'world', load_mode: 'resident' },
    ]) expect((await request.post(`${base}/items`, { data: { content: `BODY_${data.id}`, load_mode: 'auto', ...data } })).ok()).toBe(true)
    const settings = await (await request.get('/api/settings')).json()
    await request.patch('/api/settings', { data: { layer: 'user', base_revision: settings.revisions.user, changes: { theme: mode === 'writing' ? 'dark' : 'light', language: 'zh-CN' } } })
    await page.setViewportSize({ width: 1680, height: 1000 })
    await page.goto('/')
    const sidebar = page.getByLabel('工作台侧边栏')
    await sidebar.getByRole('button', { name: mode === 'writing' ? '写作' : '游戏', exact: true }).click()
    await sidebar.getByRole('button', { name: '资料库', exact: true }).click()
    await page.getByTestId('lore-library').getByRole('button', { name: '更多', exact: true }).click()
    await page.getByRole('menuitem', { name: '资料索引', exact: true }).click()
    const doc = page.getByTestId('lore-index-document')
    const cast = doc.getByTestId('lore-index-section').filter({ hasText: '港口人物' })
    const characters = doc.getByTestId('lore-index-auto-group').filter({ hasText: '角色 · 按需' })
    const trigger = (group: Locator) => group.locator('[data-slot="collapsible-trigger"]').first()
    await trigger(cast).click()
    // Editor and injected names must agree even when importance/category differ.
    await expect(cast.locator('[data-slot="card-title"]')).toHaveText(['Alpha', 'Zulu'])
    await drag(page, cast.getByTestId('lore-card-zulu'), cast.getByTestId('lore-card-alpha'))
    await expect(cast.locator('[data-slot="card-title"]')).toHaveText(['Zulu', 'Alpha'])
    await expect.poll(async () => (await snapshot()).guide.item_order?.['custom:cast']).toEqual(['zulu', 'alpha'])
    await expect(doc).toBeVisible() // Drop must not open the card.
    // Drag an expanded section from its title across a collapsed section.
    const empty = doc.getByTestId('lore-index-section').filter({ hasText: '暂未关联资料的长标题' })
    await drag(page, trigger(cast), trigger(empty))
    await expect.poll(async () => (await snapshot()).guide.group_order?.slice(0, 2)).toEqual(['custom:empty', 'custom:cast'])
    await drag(page, trigger(cast), trigger(empty))
    await expect.poll(async () => (await snapshot()).guide.group_order?.slice(0, 2)).toEqual(['custom:cast', 'custom:empty'])
    await trigger(cast).click()
    await trigger(characters).click()
    await drag(page, characters.getByTestId('lore-card-auto-zulu'), characters.getByTestId('lore-card-auto-alpha'))
    await expect.poll(async () => (await snapshot()).guide.item_order?.['automatic:auto:character']).toEqual(['auto-zulu', 'auto-alpha'])
    await trigger(characters).click()
    await drag(page, trigger(characters), trigger(cast))
    await expect.poll(async () => (await snapshot()).guide.group_order?.[0]).toBe('automatic:auto:character')
    // Header keyboard sorting remains available without adding a drag handle.
    await trigger(characters).focus()
    await page.keyboard.press('Space')
    await expect(trigger(characters)).toHaveAttribute('aria-pressed', 'true')
    await page.keyboard.press('ArrowDown')
    await expect(doc.getByRole('status').filter({ hasText: '移动到第 2 项' })).toHaveCount(1)
    await page.keyboard.press('Space')
    await expect.poll(async () => (await snapshot()).guide.group_order?.slice(0, 2)).toEqual(['custom:cast', 'automatic:auto:character'])
    const guide = (await snapshot()).guide
    const preview = await (await request.post(`${base}/index/preview`, { data: guide })).json()
    expect(preview.markdown.indexOf('- Zulu')).toBeLessThan(preview.markdown.indexOf('- Alpha'))
    expect(preview.markdown.indexOf('- Auto Zulu')).toBeLessThan(preview.markdown.indexOf('- Auto Alpha'))
    expect(preview.markdown.indexOf('## 港口人物')).toBeLessThan(preview.markdown.indexOf('## Character'))
    expect(preview.markdown.indexOf('## Character')).toBeLessThan(preview.markdown.indexOf('## 暂未关联'))
    await page.reload()
    await page.getByRole('button', { name: '资料索引', exact: true }).click()
    await trigger(cast).click()
    await expect(cast.locator('[data-slot="card-title"]')).toHaveText(['Zulu', 'Alpha'])
    await trigger(characters).click()
    await expect(characters.locator('[data-slot="card-title"]')).toHaveText(['Auto Zulu', 'Auto Alpha'])
    await expect.poll(() => characters.evaluate(group => {
      const cards = group.querySelectorAll('[data-slot="card"]')
      return cards[cards.length - 1].getBoundingClientRect().bottom <= group.getBoundingClientRect().bottom
    })).toBe(true)
    await page.screenshot({ path: testInfo.outputPath(`order-${mode}-wide.png`) })
    await page.setViewportSize({ width: 390, height: 844 })
    await expect(doc).toBeVisible()
    expect(await doc.evaluate(node => node.scrollWidth <= node.clientWidth)).toBe(true)
    await page.screenshot({ path: testInfo.outputPath(`order-${mode}-narrow.png`) })
    // A regular click still opens the item after dragging and reloading.
    await cast.getByRole('button', { name: 'Zulu', exact: true }).click()
    await expect(doc).toBeHidden()
  })
}
