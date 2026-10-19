import { mkdirSync, writeFileSync } from 'node:fs'
import { expect, test } from '../support/fixtures'
import { createAndOpenBook, createProjectFile } from '../support/api'

test('measures repeated chapter and Lore tab presentation with long documents', async ({ page, request }, testInfo) => {
  const book = await createAndOpenBook(request, 'Writing tabs performance')
  for (const name of ['alpha', 'beta']) {
    await createProjectFile(request, book.projectId, `chapters/${name}.md`, `# ${name}\n\n` + `${name}：${'车站的灯光照亮漫长的轨道。'.repeat(16)}\n\n`.repeat(180))
  }
  await page.goto('/')
  await page.getByRole('button', { name: /^alpha\b/ }).click()
  await expect(page.getByTestId('main').getByRole('textbox', { name: '', exact: true })).toContainText('alpha')
  await page.getByRole('button', { name: /^beta\b/ }).click()
  await expect(page.getByTestId('main').getByRole('textbox', { name: '', exact: true })).toContainText('beta')
  await page.getByTestId('book-settings-header-frame').getByRole('button', { name: '设定', exact: true }).click()
  await expect(page.getByRole('region', { name: '作品设定', exact: true })).toBeVisible()
  await page.evaluate(() => {
    const timing = { started: 0, painted: 0, target: '' }
    ;(window as unknown as { tabTiming: typeof timing }).tabTiming = timing
    const sample = () => {
      if (!timing.started || timing.painted) return
      const selected = document.querySelector('[data-writing-content-layer] [role="tab"][aria-selected="true"]')
      if (!selected?.textContent?.includes(timing.target)) return
      const ready = timing.target === '设定'
        ? Array.from(document.querySelectorAll('[aria-label="作品设定"]')).some(element => element.getClientRects().length)
        : Array.from(document.querySelectorAll('[data-writing-content-layer] [role="textbox"]'))
          .some(element => element.getClientRects().length && element.textContent?.includes(timing.target))
      if (ready) timing.painted = performance.now() - timing.started
    }
    document.addEventListener('pointerdown', event => {
      const tab = (event.target as Element).closest('[data-writing-content-layer] [role="tab"]')
      if (!tab) return
      timing.target = ['alpha', 'beta', '设定'].find(name => tab.textContent?.includes(name)) || ''
      timing.started = performance.now()
      timing.painted = 0
      requestAnimationFrame(sample)
    }, true)
    new MutationObserver(() => requestAnimationFrame(sample)).observe(document, { subtree: true, childList: true, attributes: true })
  })
  const timings: Record<string, number[]> = { alpha: [], beta: [], '设定': [] }
  const profiler = process.env.DENOVA_PROFILE_TABS ? await page.context().newCDPSession(page) : null
  if (profiler) {
    await profiler.send('Profiler.enable')
    await profiler.send('Profiler.start')
  }
  for (let attempt = 0; attempt < 5; attempt++) {
    for (const target of ['alpha', 'beta', '设定']) {
      await page.getByRole('tab', { name: new RegExp(target) }).click()
      await page.waitForFunction(() => (window as unknown as { tabTiming: { painted: number } }).tabTiming.painted > 0)
      timings[target].push(await page.evaluate(() => (window as unknown as { tabTiming: { painted: number } }).tabTiming.painted))
    }
  }
  mkdirSync('test-results', { recursive: true })
  if (profiler) {
    const { profile } = await profiler.send('Profiler.stop')
    writeFileSync('test-results/writing-tabs.cpuprofile', JSON.stringify(profile))
    await profiler.detach()
  }
  const path = `test-results/writing-tabs-${process.env.DENOVA_PERFORMANCE_LABEL || 'current'}.json`
  writeFileSync(path, JSON.stringify(timings, null, 2) + '\n')
  await testInfo.attach('writing-tabs', { path, contentType: 'application/json' })
})
