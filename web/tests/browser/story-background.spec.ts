import { createRequire } from 'node:module'
import { expect, test } from '../support/fixtures'
import { createAndOpenBook, createStartedStory } from '../support/api'
import type { LoreItem } from '../../src/lib/api'

for (const theme of ['dark', 'light']) {
  test(`manually replaces a dynamic background and restores it in ${theme}`, async ({ page, request }) => {
    const settings = await (await request.get('/api/settings')).json()
    const saved = await request.patch('/api/settings', { data: {
      layer: 'user', base_revision: settings.revisions.user, changes: { theme, language: 'zh-CN' },
    } })
    expect(saved.ok(), await saved.text()).toBe(true)
    const book = await createAndOpenBook(request, `Background ${theme}`)
    const url = `/api/projects/${book.projectId}/book/lore/items`
    const created = await request.post(url, { data: { id: 'station', name: '车站', type: 'location', content: 'An old station.' } })
    expect(created.ok(), await created.text()).toBe(true)
    const sharp = createRequire(import.meta.url)('sharp') as typeof import('sharp').default
    const longName = '夜晚背景LongBackgroundName'.repeat(5)
    for (const [name, background] of [['day', '#a0b8c0'], [longName, '#301020']]) {
      const image = await sharp(Buffer.from(`<svg width="320" height="180" xmlns="http://www.w3.org/2000/svg"><rect width="320" height="180" fill="${background}"/><rect width="80" height="180" fill="#657d62"/><circle cx="260" cy="45" r="24" fill="#e2bb67"/><path d="M0 140H320" stroke="#a0b8c0" stroke-width="12"/></svg>`)).png().toBuffer()
      const uploaded = await request.post(`${url}/station/materials/upload`, {
        multipart: { file: { name: `${name}.png`, mimeType: 'image/png', buffer: image } },
      })
      expect(uploaded.ok(), await uploaded.text()).toBe(true)
    }
    const items = await (await request.get(url)).json() as { items: LoreItem[] }
    const assets = items.items.find(item => item.id === 'station')!.resolved_materials!
    const story = await createStartedStory(request, '手动背景切换')
    const snapshotURL = `/api/interactive/stories/${story.id}/snapshot?branch=main`
    const snapshot = await (await request.get(snapshotURL)).json()
    const turn = snapshot.current_turn
    const changed = await request.patch(`/api/interactive/stories/${story.id}/turns/${turn.id}/background`, {
      data: { branch_id: 'main', background: { item_id: 'station', asset_id: assets[0].id } },
    })
    expect(changed.ok(), await changed.text()).toBe(true)
    await page.goto('/')
    await page.getByLabel('工作台侧边栏').getByRole('button', { name: '游戏', exact: true }).click()
    await page.getByRole('tab', { name: '控制', exact: true }).click()
    await expect(page.getByRole('button', { name: '当前背景', exact: true })).toHaveCount(1)
    await expect(page.getByRole('button', { name: '默认背景', exact: true })).toHaveCount(0)
    const dynamic = page.getByRole('switch', { name: '动态背景', exact: true })
    await dynamic.click()
    await expect(dynamic).not.toBeChecked()
    await expect(page.locator('[data-stage-layer="background"]')).toHaveAttribute('alt', assets[0].name)
    await page.getByRole('button', { name: '当前背景', exact: true }).click()
    const picker = page.getByRole('dialog', { name: '当前背景', exact: true })
    await picker.getByRole('button', { name: new RegExp(longName) }).click()
    await expect(page.locator('[data-stage-layer="background"]')).toHaveAttribute('alt', assets[1].name)
    await expect.poll(async () => (await (await request.get(snapshotURL)).json()).current_turn.turn_result.presentation.background.asset_id).toBe(assets[1].id)
    await expect(dynamic).not.toBeChecked()
    await page.getByRole('button', { name: '设置背景焦点', exact: true }).click()
    const focusDialog = page.getByRole('dialog', { name: '设置背景焦点', exact: true })
    const horizontal = focusDialog.getByRole('slider', { name: '横向焦点', exact: true })
    await expect(horizontal).toBeEnabled()
    const source = focusDialog.getByRole('button', { name: '选择焦点（可使用滑块通过键盘调整）', exact: true })
    const bounds = (await source.boundingBox())!
    const x = Math.round(bounds.x + bounds.width / 4)
    const y = Math.round(bounds.y + bounds.height * 0.75)
    await page.mouse.click(x, y)
    await expect(horizontal).toHaveAttribute('aria-valuenow', String(Math.round((x - bounds.x) / bounds.width * 100)))
    await horizontal.focus()
    await horizontal.press('End')
    await focusDialog.getByRole('slider', { name: '纵向焦点', exact: true }).press('Home')
    await page.screenshot({ path: test.info().outputPath(`focus-${theme}-wide.png`) })
    await focusDialog.getByRole('button', { name: '保存', exact: true }).click()
    await expect.poll(async () => (await (await request.get(snapshotURL)).json()).current_turn.turn_result.presentation.background.focus).toEqual({ x: 1, y: 0 })
    await page.reload()
    await expect(page.locator('[data-stage-layer="background"]')).toHaveAttribute('alt', assets[1].name)
    await expect(dynamic).not.toBeChecked()
    await page.getByRole('button', { name: '当前背景', exact: true }).scrollIntoViewIfNeeded()
    await page.screenshot({ path: test.info().outputPath(`background-${theme}-wide.png`) })
    await page.setViewportSize({ width: 390, height: 844 })
    await page.getByRole('button', { name: '显示控制台', exact: true }).click()
    const panel = page.getByRole('dialog', { name: '控制台', exact: true })
    await panel.getByRole('tab', { name: '控制', exact: true }).click()
    await expect(page.locator('[data-stage-layer="background"]')).toHaveCSS('object-position', '100% 50%')
    await panel.getByRole('button', { name: '设置背景焦点', exact: true }).click()
    await expect(focusDialog.getByRole('slider', { name: '横向焦点', exact: true })).toHaveAttribute('aria-valuenow', '100')
    await page.screenshot({ path: test.info().outputPath(`focus-${theme}-390.png`) })
    await focusDialog.getByRole('button', { name: '恢复居中', exact: true }).click()
    await focusDialog.getByRole('button', { name: '保存', exact: true }).click()
    await expect.poll(async () => (await (await request.get(snapshotURL)).json()).current_turn.turn_result.presentation.background.focus).toBeUndefined()
    await panel.getByRole('button', { name: '当前背景', exact: true }).click()
    await picker.getByRole('textbox').fill('missing background')
    await expect(picker.getByText('暂无匹配图片，请先在资料库中绑定本地图片素材。')).toBeVisible()
    await page.screenshot({ path: test.info().outputPath(`background-${theme}-390.png`) })
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
    await picker.getByRole('button', { name: '无背景', exact: true }).click()
    await expect.poll(async () => (await (await request.get(snapshotURL)).json()).current_turn.turn_result.presentation.background).toBeUndefined()
    const after = await (await request.get(snapshotURL)).json()
    expect(after.current_turn.narrative).toBe(turn.narrative)
    expect(after.current_turn.turn_result.choices).toEqual(turn.turn_result.choices)
  })
}
