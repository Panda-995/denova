import { expect, test } from '../support/fixtures'
import { createAndOpenBook, createStartedStory } from '../support/api'

for (const { theme, locale, width } of [
  { theme: 'dark', locale: 'zh-CN', width: 1440 },
  { theme: 'light', locale: 'en-US', width: 1440 },
  { theme: 'dark', locale: 'en-US', width: 390 },
  { theme: 'light', locale: 'zh-CN', width: 390 },
] as const) {
  test(`usage notice and settings cover writing/game in ${locale}, ${theme}, ${width}px`, async ({ page, request }) => {
    test.setTimeout(60_000)
    await createAndOpenBook(request, 'Analytics privacy validation')
    await createStartedStory(request, 'Analytics game validation')
    const settings = await (await request.get('/api/settings')).json()
    await page.route(/\/api\/(?:projects\/[^/]+\/)?settings$/, route => route.fulfill({
      json: { ...settings, effective: { ...settings.effective, theme, language: locale } },
    }))
    let preferences = { enabled: true, notice_seen: false, available: true }
    const activities: unknown[] = []
    await page.route(/\/api\/analytics$/, async route => {
      if (route.request().method() === 'PATCH') {
        const body = route.request().postDataJSON()
        expect(Object.keys(body)).toEqual(['enabled'])
        preferences = { ...preferences, enabled: body.enabled, notice_seen: true }
      }
      await route.fulfill({ json: preferences })
    })
    await page.route('**/api/analytics/activity', async route => {
      activities.push(route.request().postDataJSON())
      await route.fulfill({ json: {} })
    })
    await page.setViewportSize({ width, height: 900 })
    await page.goto('/')
    const chinese = locale === 'zh-CN'
    const selectDestination = async (name: string) => {
      if (width < 768) {
        await page.getByRole('button', { name: chinese ? '导航菜单' : 'Navigation', exact: true }).click()
        await page.getByRole('dialog', { name: chinese ? '导航菜单' : 'Navigation', exact: true }).getByRole('button', { name, exact: true }).click()
      } else {
        await page.getByRole('navigation').first().getByRole('button', { name, exact: true }).click()
      }
    }
    const dialog = page.getByRole('dialog', { name: chinese ? '使用统计不包含创作内容' : 'Usage statistics without creative content' })
    await expect(dialog).toBeVisible()
    await expect(dialog).toContainText(chinese ? '默认开启' : 'enabled by default')
    await expect(dialog).toContainText(chinese ? '执行结果和耗时' : 'execution outcomes and duration')
    await expect(dialog).toContainText(chinese ? '不会发送正文' : 'No creative text')
    expect(activities).toEqual([])
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
    await page.screenshot({ path: test.info().outputPath(`analytics-notice-${locale}-${theme}-${width}.png`) })
    await dialog.getByRole('button', { name: chinese ? '知道了' : 'Got it', exact: true }).click()
    await expect(dialog).toBeHidden()
    expect(preferences).toEqual({ enabled: true, notice_seen: true, available: true })
    await selectDestination(chinese ? '设置' : 'Settings')
    const toggle = page.getByRole('switch', { name: chinese ? '分享使用统计' : 'Share usage statistics', exact: true })
    await expect(toggle).toBeChecked()
    await expect(page.getByText(chinese
      ? '向 Umami Cloud 分享活跃、功能触达、留存、版本、平台、执行结果和耗时。'
      : 'Share activity, feature reach, return visits, app version, platform, execution outcomes and duration with Umami Cloud.', { exact: false })).toBeVisible()
    await toggle.click()
    await expect(toggle).not.toBeChecked()
    await page.screenshot({ path: test.info().outputPath(`analytics-settings-${locale}-${theme}-${width}.png`) })
    await page.reload()
    await expect(dialog).toBeHidden()
    await expect(toggle).not.toBeChecked()
    await selectDestination(chinese ? '游戏' : 'Game')
    // The settings panel is shared across the two creation destinations.
    await expect(toggle).toBeHidden()
    await selectDestination(chinese ? '设置' : 'Settings')
    await expect(toggle).not.toBeChecked()
    await toggle.click()
    await expect(toggle).toBeChecked()
    expect(preferences.enabled).toBe(true)
  })
}
