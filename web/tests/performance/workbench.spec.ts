import { mkdirSync, writeFileSync } from 'node:fs'
import { performance } from 'node:perf_hooks'
import { expect, test } from '../support/fixtures'
import { createAndOpenBook } from '../support/api'

test('measures startup, every primary destination, and shared API reads', async ({ page, request }, testInfo) => {
  const book = await createAndOpenBook(request, 'Performance Book')
  const chapterContent = '# Performance chapter\n\n' + '这是用于性能验证的长篇正文。The story continues.\n'.repeat(100)
  const seeded = await request.post(`/api/projects/${book.projectId}/files/operations`, { data: {
    operations: Array.from({ length: 100 }, (_, index) => ({
      id: String(index), kind: 'create', type: 'file',
      path: `chapters/ch${index + 1}-performance.md`, content: chapterContent,
    })),
  } })
  expect(seeded.ok()).toBe(true)
  expect((await seeded.json()).results.every((result: { ok: boolean }) => result.ok)).toBe(true)
  const settings = await (await request.get('/api/settings')).json()
  await page.route(/\/api\/(?:projects\/[^/]+\/)?settings$/, route => route.fulfill({
    json: { ...settings, effective: { ...settings.effective, labs: { ...settings.effective?.labs, developer_mode: true } } },
  }))
  await page.route(/\/api\/agent-runs(?:\?|$)/, route => route.fulfill({ json: { runs: [], issues: [] } }))
  await page.route('**/api/resource-market/catalog', route => route.fulfill({ json: { schema_version: 1, entries: [] } }))
  await page.addInitScript(() => {
    const measurements = { startupMs: 0, navigationMs: 0 }
    ;(window as unknown as { denovaPerformance: typeof measurements }).denovaPerformance = measurements
    let navigation: { started: number; previous: Element | null } | undefined
    let framePending = false
    const section = () => document.querySelector('[data-testid="main"] section[aria-hidden="false"]')
    const ready = () => !Array.from(document.querySelectorAll('[data-slot="loading-state"]'))
      .some(element => element.getClientRects().length > 0)
    const sample = () => {
      if (framePending) return
      framePending = true
      requestAnimationFrame(() => {
        framePending = false
        if (!ready()) return
        if (!measurements.startupMs && document.querySelector('[aria-label="作品大纲"] button')) {
          measurements.startupMs = performance.now()
        }
        if (navigation && section() !== navigation.previous) {
          measurements.navigationMs = performance.now() - navigation.started
          navigation = undefined
        }
      })
    }
    document.addEventListener('click', event => {
      if (!(event.target instanceof Element) || !event.target.closest('[aria-label="工作台侧边栏"] button')) return
      measurements.navigationMs = 0
      navigation = { started: performance.now(), previous: section() }
      sample()
    }, true)
    new MutationObserver(sample).observe(document, { subtree: true, childList: true, attributes: true })
  })

  const startup: Array<Record<string, number>> = []
  for (let attempt = 0; attempt < 5; attempt++) {
    await page.goto('/')
    await expect(page.getByLabel('工作台侧边栏')).toBeVisible()
    await expect(page.locator('[data-slot=loading-state]:visible')).toHaveCount(0)
    await expect(page.getByRole('button', { name: /^1 performance\b/ })).toBeVisible()
    startup.push(await page.evaluate(() => {
      const resources = performance.getEntriesByType('resource') as PerformanceResourceTiming[]
      const scripts = resources.filter(entry => /\/assets\/.*\.js(?:\?|$)/.test(entry.name))
      const navigation = performance.getEntriesByType('navigation')[0] as PerformanceNavigationTiming
      return {
        readyMs: (window as unknown as { denovaPerformance: { startupMs: number } }).denovaPerformance.startupMs,
        domContentLoadedMs: navigation.domContentLoadedEventEnd,
        scriptBytes: scripts.reduce((sum, entry) => sum + entry.decodedBodySize, 0), scriptRequests: scripts.length,
      }
    }))
  }

  const startupScripts = await page.evaluate(() => (performance.getEntriesByType('resource') as PerformanceResourceTiming[])
    .filter(entry => /\/assets\/.*\.js(?:\?|$)/.test(entry.name))
    .map(entry => ({ name: new URL(entry.name).pathname, bytes: entry.decodedBodySize }))
    .sort((left, right) => right.bytes - left.bytes))

  const destinations = ['游戏', '资料库', '创作方案', '资源中心', '工作台', '书籍管理', '版本管理', 'Skills', 'Agents', '自动化', '轨迹', '设置', '写作']
  const coldRoutes: Record<string, number[]> = {}
  const warmRoutes: Record<string, number[]> = {}
  const sidebar = page.getByLabel('工作台侧边栏')
  for (let attempt = 0; attempt < 3; attempt++) {
    if (attempt > 0) {
      await page.goto('/')
      await expect(sidebar).toBeVisible()
      await expect(page.getByRole('button', { name: /^1 performance\b/ })).toBeVisible()
      await expect(page.locator('[data-slot=loading-state]:visible')).toHaveCount(0)
    }
    for (const phase of ['cold', 'warm'] as const) {
      for (const destination of destinations) {
        const oldSection = await page.getByTestId('main').locator('section[aria-hidden="false"]').first().elementHandle()
        await sidebar.getByRole('button', { name: destination, exact: true }).click()
        await expect(sidebar.locator('[aria-current="page"]')).toHaveCount(1)
        await page.waitForFunction(previous => document.querySelector('[data-testid="main"] section[aria-hidden="false"]') !== previous, oldSection)
        await expect(page.locator('[data-slot=loading-state]:visible')).toHaveCount(0)
        await page.evaluate(() => new Promise<void>(resolve => requestAnimationFrame(() => requestAnimationFrame(() => resolve()))))
        const elapsed = await page.evaluate(() => (window as unknown as { denovaPerformance: { navigationMs: number } }).denovaPerformance.navigationMs)
        expect(elapsed).toBeGreaterThan(0)
        const routes = phase === 'cold' ? coldRoutes : warmRoutes
        ;(routes[destination] ??= []).push(elapsed)
      }
    }
  }

  const api: Record<string, number[]> = {}
  for (const path of [
    '/api/settings', '/api/books',
    `/api/projects/${book.projectId}/book/tree`, `/api/projects/${book.projectId}/book/summary`,
    `/api/projects/${book.projectId}/book/lore/items`, `/api/projects/${book.projectId}/book/lore/categories`,
    '/api/interactive/stories', '/api/sessions',
  ]) {
    const key = path.replace(book.projectId, ':project')
    api[key] = []
    for (let attempt = 0; attempt < 9; attempt++) {
      const started = performance.now()
      const response = await request.get(path)
      expect(response.ok(), await response.text()).toBe(true)
      api[key].push(performance.now() - started)
    }
  }
  const scripts = await page.evaluate(() => (performance.getEntriesByType('resource') as PerformanceResourceTiming[])
    .filter(entry => /\/assets\/.*\.js(?:\?|$)/.test(entry.name))
    .map(entry => ({ name: new URL(entry.name).pathname, bytes: entry.decodedBodySize }))
    .sort((left, right) => right.bytes - left.bytes))
  const report = { platform: process.platform, startup, coldRoutes, warmRoutes, api, startupScripts, scripts }
  mkdirSync('test-results', { recursive: true })
  const output = `test-results/performance-${process.env.DENOVA_PERFORMANCE_LABEL || 'current'}.json`
  writeFileSync(output, JSON.stringify(report, null, 2) + '\n')
  await testInfo.attach('performance', { path: output, contentType: 'application/json' })
})
