import { expect, test } from '../support/fixtures'
import { createAndOpenBook } from '../support/api'
import { openWritingAgent } from '../support/agent-chat'

for (const theme of ['dark', 'light'] as const) {
  test(`Agent selector keeps its selected row aligned with the trigger in ${theme}`, async ({ page, request }) => {
    await createAndOpenBook(request, `Agent selector ${theme}`)
    const settings = await (await request.get('/api/settings')).json()
    const longName = 'An Agent with a very long name for writing and reviewing chapters'
    let customAgents: Array<{ id: string; name: string; contract: string }> = []
    await page.route(/\/api\/(?:projects\/[^/]+\/)?settings$/, route => route.fulfill({ json: {
      ...settings, effective: { ...settings.effective, theme, custom_agents: customAgents },
    } }))

    for (const width of [1440, 390]) {
      await page.setViewportSize({ width, height: 900 })
      for (const populated of [false, true]) {
        customAgents = populated ? [{ id: 'long-writing-agent', name: longName, contract: 'writing.primary.v1' }] : []
        await page.goto('/')
        if (width === 390) {
          await page.getByRole('tab', { name: 'Agent', exact: true }).click()
        } else await openWritingAgent(page)
        const trigger = page.getByRole('combobox', { name: 'Agent', exact: true })
        await expect(trigger).toBeEnabled()
        const before = await trigger.evaluate(el => ({
          rect: el.getBoundingClientRect().toJSON(),
          fontSize: getComputedStyle(el).fontSize,
          lineHeight: getComputedStyle(el).lineHeight,
        }))
        await trigger.click()
        const list = page.getByRole('listbox')
        await expect(list).toBeVisible()
        const selected = page.getByRole('option', { name: '写作 Agent', exact: true })
        const after = await selected.evaluate(el => ({
          rect: el.getBoundingClientRect().toJSON(),
          fontSize: getComputedStyle(el).fontSize,
          lineHeight: getComputedStyle(el).lineHeight,
        }))
        expect(after.fontSize).toBe(before.fontSize)
        expect(after.lineHeight).toBe(before.lineHeight)
        expect(await selected.evaluate(el => {
          const text = el.lastElementChild as HTMLElement
          return text.scrollWidth <= text.clientWidth
        })).toBe(true)
        for (const dimension of ['x', 'y', 'width', 'height'] as const) {
          expect(Math.abs(after.rect[dimension] - before.rect[dimension]), dimension).toBeLessThanOrEqual(1)
        }
        if (populated) {
          const option = page.getByRole('option', { name: longName, exact: true })
          await expect(option).toBeVisible()
          expect(await option.evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true)
        }
        expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
        await page.screenshot({ path: test.info().outputPath(`agent-select-${theme}-${width}-${populated}.png`), animations: 'disabled' })
        await selected.click()
        await expect(list).toBeHidden()
        await expect(trigger).toHaveText('写作 Agent')
      }
    }
  })
}
