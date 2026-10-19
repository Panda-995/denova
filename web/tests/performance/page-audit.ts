import { mkdirSync, writeFileSync } from 'node:fs'
import { performance } from 'node:perf_hooks'
import { expect, type APIRequestContext, type Page } from '../support/fixtures'

type Ready = { selector: string; text?: string; count?: number }

// Keep timing in the page: Playwright's assertion polling is not user latency.
export async function installAudit(page: Page) {
  await page.addInitScript(() => {
    const longTasks: Array<{ start: number; duration: number }> = []
    new PerformanceObserver(list => list.getEntries().forEach(entry => longTasks.push({ start: entry.startTime, duration: entry.duration })))
      .observe({ type: 'longtask', buffered: true })
    const audit = { armed: false, start: 0, end: 0, ready: { selector: '', text: '', count: 1 }, longTasks }
    ;(window as any).__pageAudit = audit
    const sample = () => {
      if (!audit.start || audit.end) return
      const nodes = Array.from(document.querySelectorAll(audit.ready.selector))
        .filter(node => node.getClientRects().length && (!audit.ready.text || node.textContent?.includes(audit.ready.text)))
      const loading = Array.from(document.querySelectorAll('[data-slot="loading-state"]')).some(node => node.getClientRects().length)
      if (nodes.length >= audit.ready.count && !loading) {
        requestAnimationFrame(() => { audit.end = performance.now() })
      } else requestAnimationFrame(sample)
    }
    const start = () => {
      if (!audit.armed) return
      audit.armed = false
      audit.start = performance.now()
      requestAnimationFrame(sample)
    }
    document.addEventListener('click', start, true)
    document.addEventListener('input', start, true)
    performance.setResourceTimingBufferSize(10000)
  })
}

export async function measure(page: Page, ready: Ready, action: () => Promise<unknown>) {
  await page.evaluate(ready => {
    Object.assign((window as any).__pageAudit, { armed: true, start: 0, end: 0, ready: { text: '', count: 1, ...ready } })
  }, ready)
  await action()
  await page.waitForFunction(() => (window as any).__pageAudit.end > 0, undefined, { timeout: 60000 })
  // Include deferred cache revalidation immediately after the first content frame.
  await page.evaluate(() => new Promise(resolve => setTimeout(resolve, 350)))
  return page.evaluate(() => {
    const { start, end, longTasks } = (window as any).__pageAudit
    const resources = (performance.getEntriesByType('resource') as PerformanceResourceTiming[])
      .filter(entry => entry.startTime >= start && entry.startTime <= end + 350)
    const tasks = longTasks.filter((entry: { start: number; duration: number }) => entry.start < end + 350 && entry.start + entry.duration > start)
    return {
      readyMs: end - start,
      maxLongTaskMs: Math.max(0, ...tasks.map((entry: { duration: number }) => entry.duration)),
      postReadyLongTaskMs: Math.max(0, ...tasks.filter((entry: { start: number }) => entry.start >= end).map((entry: { duration: number }) => entry.duration)),
      blockingMs: tasks.reduce((sum: number, entry: { duration: number }) => sum + Math.max(0, entry.duration - 50), 0),
      domNodes: document.querySelectorAll('*').length,
      requests: resources.filter(entry => new URL(entry.name).pathname.startsWith('/api/')).map(entry => ({
        path: new URL(entry.name).pathname + new URL(entry.name).search,
        startMs: entry.startTime - start, durationMs: entry.duration, bytes: entry.decodedBodySize,
      })),
      scriptBytes: resources.filter(entry => /\/assets\/.*\.js/.test(entry.name)).reduce((sum, entry) => sum + entry.decodedBodySize, 0),
    }
  })
}

export async function visit(page: Page, destination: string, ready: Ready, samples = 3) {
  const result: { cold: unknown[]; warm: unknown[] } = { cold: [], warm: [] }
  for (let attempt = 0; attempt < samples; attempt++) {
    await page.goto('/')
    const sidebar = page.getByLabel('工作台侧边栏')
    await expect(sidebar).toBeVisible()
    await expect(page.locator('[data-slot=loading-state]:visible')).toHaveCount(0)
    // Navigation selection persists in localStorage; always leave through Writing.
    await sidebar.getByRole('button', { name: '写作', exact: true }).click()
    await expect(page.getByLabel('作品大纲')).toBeVisible()
    result.cold.push(await measure(page, ready, () => sidebar.getByRole('button', { name: destination, exact: true }).click()))
    await sidebar.getByRole('button', { name: '写作', exact: true }).click()
    await expect(page.getByLabel('作品大纲')).toBeVisible()
    result.warm.push(await measure(page, ready, () => sidebar.getByRole('button', { name: destination, exact: true }).click()))
    console.log(`Measured destination=${destination} attempt=${attempt + 1}`)
  }
  return result
}

export async function probe(request: APIRequestContext, path: string, attempts = 7) {
  const result = []
  for (let attempt = 0; attempt < attempts; attempt++) {
    const start = performance.now()
    const response = await request.get(path)
    const body = await response.body()
    expect(response.ok(), body.toString().slice(0, 500)).toBe(true)
    result.push({ ms: performance.now() - start, bytes: body.length })
  }
  return result
}

export function saveAudit(name: string, result: unknown) {
  mkdirSync('test-results', { recursive: true })
  const label = process.env.DENOVA_PERFORMANCE_LABEL?.replace(/[^a-zA-Z0-9_-]/g, '')
  writeFileSync(`test-results/page-audit-${label ? `${label}-` : ''}${name}.json`, JSON.stringify(result, null, 2) + '\n')
}
