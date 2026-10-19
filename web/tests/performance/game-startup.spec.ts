import { mkdirSync, writeFileSync } from 'node:fs'
import { expect, test } from '../support/fixtures'
import { createAndOpenBook, createStartedStory, getStorySnapshot } from '../support/api'

test('measures first Game entry for new and existing stories', async ({ page, request }, testInfo) => {
  await createAndOpenBook(request, 'Game startup performance')
  await page.addInitScript(() => localStorage.setItem('nova:mode', 'ide'))
  const measurements: Record<string, unknown[]> = { empty: [], existing: [] }
  for (const scenario of ['empty', 'existing'] as const) {
    const story = scenario === 'existing' ? await createStartedStory(request, 'Startup story') : null
    const narrative = story ? (await getStorySnapshot(request, story.id)).turns[0].narrative : ''
    for (let attempt = 0; attempt < 3; attempt++) {
      await page.goto('/')
      const sidebar = page.getByLabel('工作台侧边栏')
      await expect(sidebar).toBeVisible()
      await expect(page.locator('[data-slot=loading-state]:visible')).toHaveCount(0)
      const started = await page.evaluate(() => performance.now())
      await sidebar.getByRole('button', { name: '游戏', exact: true }).click()
      if (story) {
        await expect(page.getByText(narrative, { exact: true })).toBeVisible()
        await page.getByPlaceholder(/你要做什么/).fill('Continue')
        await expect(page.locator('[data-action="send"]').filter({ visible: true })).toBeEnabled()
      } else {
        await expect(page.getByRole('button', { name: '开始故事', exact: true })).toBeEnabled()
      }
      measurements[scenario].push(await page.evaluate(start => {
        const entries = performance.getEntriesByType('resource') as PerformanceResourceTiming[]
        const resources = entries.filter(entry => entry.startTime >= start)
        const scripts = resources.filter(entry => /\/assets\/.*\.js(?:\?|$)/.test(entry.name))
        return {
          readyMs: performance.now() - start,
          scriptBytes: scripts.reduce((sum, entry) => sum + entry.decodedBodySize, 0),
          scripts: scripts.map(entry => ({ name: new URL(entry.name).pathname, bytes: entry.decodedBodySize })),
          requests: resources.filter(entry => entry.name.includes('/api/')).map(entry => ({
            path: new URL(entry.name).pathname, startMs: entry.startTime - start, durationMs: entry.duration,
          })),
        }
      }, started))
    }
  }
  mkdirSync('test-results', { recursive: true })
  const path = `test-results/game-startup-${process.env.DENOVA_PERFORMANCE_LABEL || 'current'}.json`
  writeFileSync(path, JSON.stringify(measurements, null, 2) + '\n')
  await testInfo.attach('game-startup', { path, contentType: 'application/json' })
})
