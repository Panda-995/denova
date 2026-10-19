import { expect, test } from '../support/fixtures'
import { createAndOpenBook } from '../support/api'
import type { CatalogEntry, Instance, RuntimeSnapshot } from '../../src/features/platform/api'

for (const theme of ['dark', 'light'] as const) {
  test(`story picker scrolls inside the game menu in ${theme}`, async ({ page, request }) => {
    const book = await createAndOpenBook(request, `Story picker ${theme}`)
    const instances: Instance[] = Array.from({ length: 30 }, (_, index) => ({
      instanceId: `scroll-story-${index}`, gameId: 'scroll-game', releaseId: 'release',
      title: `Story ${String(index).padStart(2, '0')} — A long storyline title for scrolling`,
      projectId: book.projectId, dependencies: [], models: {}, setup: {}, preview: false,
      createdAt: new Date(Date.UTC(2026, 0, 30 - index)).toISOString(),
    }))
    const runtime: RuntimeSnapshot = {
      id: 'scroll-runtime', status: 'running',
      viewUrl: new URL('/scroll-game-frame', test.info().project.use.baseURL).href,
      connection: { baseUrl: '/unused', token: 'test' },
      context: {
        source: { package: { kind: 'game', id: 'scroll-game' }, releaseId: 'release' },
        scope: { kind: 'project', projectId: book.projectId },
        locale: 'zh-CN', theme, environment: 'production', settings: {},
      },
    }
    const settings = await (await request.get('/api/settings')).json()
    const game: CatalogEntry = {
      kind: 'game', id: 'scroll-game', enabled: true, currentRelease: 'release', grants: [],
      releases: [{
        ref: { package: { kind: 'game', id: 'scroll-game' }, releaseId: 'release' },
        digest: 'scroll-fixture', installedAt: instances[0].createdAt,
        manifest: {
          id: 'scroll-game', version: '1.0.0', apiMajor: 1, minHostVersion: '0.6.0',
          name: { 'zh-CN': '滚动测试游戏', 'en-US': 'Scroll fixture game' },
          permissions: { required: [], optional: [] },
          game: { viewId: 'game', storage: { kind: 'self' } },
        },
      }],
    }
    await page.route(/\/api\/(?:projects\/[^/]+\/)?settings$/, route => route.fulfill({
      json: { ...settings, effective: { ...settings.effective, theme } },
    }))
    await page.route('**/api/platform/manage/instances', route => route.fulfill({ json: instances }))
    await page.route('**/api/platform/manage/catalog', route => route.fulfill({ json: [game] }))
    await page.route('**/api/platform/manage/instances/*/open', route => route.fulfill({ json: runtime }))
    await page.route('**/api/platform/manage/runtimes', route => route.fulfill({ json: [runtime] }))
    await page.route('**/scroll-game-frame', route => route.fulfill({ contentType: 'text/html', body: '<main>Game</main>' }))
    await page.addInitScript(({ projectId, instanceId }) => {
      localStorage.setItem(`denova.game-story-selection.${projectId}`, instanceId)
    }, { projectId: book.projectId, instanceId: instances[0].instanceId })
    await page.goto('/')
    await page.getByLabel('工作台侧边栏').getByRole('button', { name: '游戏', exact: true }).click()
    await expect(page.getByRole('button', { name: '故事与存档', exact: true })).toBeVisible()

    for (const width of [1440, 390]) {
      await page.setViewportSize({ width, height: 800 })
      await page.getByRole('button', { name: '故事与存档', exact: true }).click()
      const trigger = page.getByRole('button', { name: '选择故事线', exact: true }).filter({ visible: true })
      await trigger.click()
      const popup = page.locator('[data-slot="popover-content"]')
      const list = popup.locator('div[aria-label="选择故事线"]')
      await expect(list).toBeVisible()
      expect(await list.evaluate(element => element.scrollHeight > element.clientHeight)).toBe(true)
      await list.hover()
      await page.mouse.wheel(0, 500)
      await expect.poll(() => list.evaluate(element => element.scrollTop)).toBeGreaterThan(100)
      const position = await list.evaluate(element => element.scrollTop)
      await page.mouse.wheel(0, -250)
      await expect.poll(() => list.evaluate(element => element.scrollTop)).toBeLessThan(position)
      await expect(popup.getByRole('button', { name: '重命名当前故事线' })).toBeInViewport()
      await page.screenshot({ path: test.info().outputPath(`story-picker-${width}.png`) })
      await page.keyboard.press('Escape')
      await expect(popup).toHaveCount(0)
      await expect(trigger).toBeFocused()
      await trigger.click()
      await page.mouse.move((await list.boundingBox())!.x + 50, (await list.boundingBox())!.y + 50)
      await page.mouse.wheel(0, 10000)
      const selected = instances.at(width === 1440 ? -1 : -2)!
      const last = popup.getByRole('button', { name: selected.title, exact: true })
      await expect(last).toBeInViewport()
      await last.click()
      await expect(popup).toHaveCount(0)
      // Switching instances remounts the player and closes its story menu.
      await page.getByRole('button', { name: '故事与存档', exact: true }).click()
      await expect(trigger).toHaveAttribute('title', selected.title)
      await page.getByRole('button', { name: '关闭', exact: true }).click()
    }
  })
}
