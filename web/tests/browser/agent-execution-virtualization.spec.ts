import { expect, test } from '../support/fixtures'
import { createAndOpenBook, createStartedStory } from '../support/api'

for (const product of ['writing', 'game'] as const) {
  for (const [theme, language] of [['dark', 'zh-CN'], ['light', 'en-US']] as const) {
    test(`${product} virtualizes a long execution and retains tool inspection in ${theme}`, async ({ page, request }) => {
      page.setDefaultTimeout(10_000)
      await createAndOpenBook(request, `Virtual execution ${product} ${theme}`)
      if (product === 'game') await createStartedStory(request, 'Virtual execution')
      const settings = await (await request.get('/api/settings')).json()
      await page.route(/\/api\/(?:projects\/[^/]+\/)?settings$/, route => route.fulfill({ json: { ...settings, effective: { ...settings.effective, theme, language } } }))
      const events = Array.from({ length: 300 }, (_, index) => ({
        id: `virtual-tool-${index}`, role: 'tool_call', name: 'bash', run_id: 'virtual-run',
        args: JSON.stringify({ command: `echo virtual-tool-${index}` }), result: `Completed step ${index}`, status: 'success',
      }))
      const final = { id: 'virtual-final', role: 'assistant', content: 'All 300 steps completed.', run_id: 'virtual-run', display_phase: 'final' }
      if (product === 'writing') {
        await page.route('**/session/messages?*', route => route.fulfill({ json: {
          messages: [{ id: 'cumulative-execution', role: 'assistant', metadata: { run_id: 'virtual-run' }, parts: events.map(event => ({
            type: 'dynamic-tool', toolName: event.name, toolCallId: event.id, state: 'output-available', input: JSON.parse(event.args), output: event.result,
          })) }, { id: final.id, role: 'assistant', metadata: { run_id: final.run_id, display_phase: 'final' }, parts: [{ type: 'text', text: final.content, state: 'done' }] }],
          page: { has_more: false, total: 2 },
        } }))
      } else {
        const displayEvents = [...events, { role: 'narrative' }]
        await page.route('**/api/interactive/stories/*/snapshot**', async route => {
          const response = await route.fetch()
          const snapshot = await response.json()
          await route.fulfill({ response, json: { ...snapshot, turns: snapshot.turns.map((turn: Record<string, unknown>, index: number) => index === snapshot.turns.length - 1 ? { ...turn, run_id: 'virtual-run', narrative: final.content, display_events: displayEvents } : turn) } })
        })
        // Expansion must load the same synthetic evidence as the snapshot.
        await page.route('**/api/interactive/stories/*/history/execution?**', async route => {
          const response = await route.fetch()
          await route.fulfill({ response, json: { ...await response.json(), display_events: displayEvents } })
        })
      }
      for (const width of [1440, 390]) {
        await page.setViewportSize({ width, height: 900 })
        if (width === 1440) await page.goto('/')
        else await page.reload()
        if (width === 1440) await page.getByLabel(/工作台侧边栏|Workbench sidebar/).getByRole('button', { name: product === 'game' ? /^(游戏|Game)$/ : /^(写作|Writing)$/ }).click()
        if (product === 'writing') {
          if (width === 390) await page.getByRole('tab', { name: 'Agent', exact: true }).click()
          else {
            const show = page.getByRole('button', { name: /^(显示创作 Agent|Show Writing Agent)$/ })
            const editor = page.getByTestId('right').locator('[contenteditable="true"]').filter({ visible: true })
            await expect(show.or(editor)).toHaveCount(1)
            if (await show.count()) await show.click()
          }
        }
        await expect(page.getByText(final.content, { exact: true })).toBeVisible()
        const header = page.locator('[data-agent-execution-process]').filter({ hasText: '300' }).getByRole('button')
        await header.click()
        const canvas = page.locator('.nova-chat-canvas:visible').last()
        await expect.poll(() => canvas.locator('[data-nova-tool-header]').count()).toBeGreaterThan(0)
        expect(await canvas.locator('[data-nova-tool-header]').count()).toBeLessThan(80)
        await canvas.evaluate(element => { element.scrollTop = 0 })
        const first = canvas.locator('[data-nova-chat-item="message"][data-nova-chat-row-key*="virtual-tool-0"]').first()
        await expect(first).toBeVisible()
        await first.locator('[data-nova-tool-header]').click()
        await expect(first.locator('[data-nova-tool-header]')).toHaveAttribute('aria-expanded', 'true')
        await first.getByRole('button', { name: /查看工具详情|View tool details/ }).click()
        const dialog = page.getByRole('dialog')
        await expect(dialog).toBeVisible()
        // The originating row must really unmount while its dialog remains open.
        await canvas.evaluate(element => { element.scrollTop = element.scrollHeight })
        await expect(first).toHaveCount(0)
        await expect(dialog).toBeVisible()
        await expect(dialog.getByText('Completed step 0', { exact: true })).toBeVisible()
        await dialog.getByRole('button', { name: /^(关闭|Close)$/ }).click()
        await expect(page.getByText(final.content, { exact: true })).toBeVisible()
        await canvas.evaluate(element => { element.scrollTop = 0 })
        await expect(first.locator('[data-nova-tool-header]')).toHaveAttribute('aria-expanded', 'true')
        await expect(header).toHaveAttribute('aria-expanded', 'true')
        await header.click()
        await expect(canvas.locator('[data-nova-tool-header]')).toHaveCount(0)
        await expect(page.getByText(final.content, { exact: true })).toBeVisible()
        expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
        await page.screenshot({ path: test.info().outputPath(`${product}-${theme}-${width}.png`), animations: 'disabled' })
      }
    })
  }
}
