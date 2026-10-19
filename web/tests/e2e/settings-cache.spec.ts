import { expect, test } from '../support/fixtures'
import { createAndOpenBook } from '../support/api'

for (const theme of ['dark', 'light'] as const) {
  test(`retains settings across Writing and Game and reconciles background changes (${theme})`, async ({ page, request }) => {
    await createAndOpenBook(request, 'Settings Cache')
    const seeded = await request.patch('/api/settings', { data: {
      layer: 'user', changes: { theme, max_open_tabs: 7, chapter_group_min: 3 },
    } })
    expect(seeded.ok()).toBe(true)
    await page.goto('/')
    const sidebar = page.getByLabel('工作台侧边栏')
    const content = page.locator('[data-nova-settings-content]')
    await sidebar.getByRole('button', { name: '设置', exact: true }).click()
    const tabs = content.getByRole('spinbutton', { name: '最大同时打开 Tab 数', exact: true })
    await expect(tabs).toHaveValue('7')
    const retained = await content.elementHandle()

    for (const [index, destination] of ['写作', '游戏'].entries()) {
      await sidebar.getByRole('button', { name: destination, exact: true }).click()
      await expect(content).toBeHidden()
      const external = await request.patch('/api/settings', { data: {
        layer: 'user', changes: { chapter_group_min: 4 + index },
      } })
      expect(external.ok()).toBe(true)

      let release!: () => void
      const gate = new Promise<void>(resolve => { release = resolve })
      let reads = 0
      await page.route('**/api/settings', async route => {
        if (route.request().method() === 'GET') {
          reads++
          await gate
        }
        await route.continue()
      })
      try {
        await sidebar.getByRole('button', { name: '设置', exact: true }).click()
        await expect(content).toBeVisible()
        expect(await retained!.evaluate(element => element === document.querySelector('[data-nova-settings-content]'))).toBe(true)
        await expect(tabs).toHaveValue(String(index === 0 ? 7 : 8))
        await expect(page.locator('[data-slot=loading-state]:visible')).toHaveCount(0)
        await expect.poll(() => reads).toBe(1)
        // An edit accepted while revalidation is pending must survive its response.
        await tabs.fill(String(8 + index))
      } finally {
        release()
      }
      await expect(content.getByRole('spinbutton', { name: '章节组最少章节', exact: true })).toHaveValue(String(4 + index))
      await expect(tabs).toHaveValue(String(8 + index))
      await expect.poll(async () => (await (await request.get('/api/settings')).json()).user.max_open_tabs).toBe(8 + index)
      await page.unrouteAll({ behavior: 'wait' })
    }
  })
}
