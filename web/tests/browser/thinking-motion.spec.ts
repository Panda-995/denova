import type { UIMessageChunk } from 'ai'
import { expect, test } from '../support/fixtures'
import { createAndOpenBook } from '../support/api'
import { openWritingAgent } from '../support/agent-chat'

for (const theme of ['dark', 'light']) {
  test(`thinking word animation follows explicit full motion in ${theme}`, async ({ page, request }) => {
    test.setTimeout(90_000)
    await createAndOpenBook(request, `Thinking motion ${theme}`)
    const settings = await (await request.get('/api/settings')).json()
    await page.route(/\/api\/(?:projects\/[^/]+\/)?settings$/, async route => {
      const response = await route.fetch()
      const body = await response.json()
      return route.fulfill({ response, json: { ...body, effective: { ...body.effective, theme } } })
    })
    await request.patch('/api/settings', { data: {
      layer: 'user', base_revision: settings.revisions.user, changes: { motion_intensity: 'system' },
    } })
    await page.addInitScript(() => {
      const originalFetch = window.fetch.bind(window)
      window.fetch = async (...args) => {
        const response = await originalFetch(...args)
        if (!response.headers.has('x-test-thinking-motion')) return response
        const bytes = new Uint8Array(await response.arrayBuffer())
        let listener: EventListener
        return new Response(new ReadableStream({
          start(controller) {
            controller.enqueue(bytes)
            listener = event => controller.enqueue(new TextEncoder().encode((event as CustomEvent<string>).detail))
            window.addEventListener('test-thinking-motion', listener)
          },
          cancel() { window.removeEventListener('test-thinking-motion', listener) },
        }), { headers: { 'content-type': 'text/event-stream' } })
      }
    })
    const chunks: UIMessageChunk[] = [
      { type: 'start', messageId: 'motion-run', messageMetadata: { run_id: 'motion-run' } },
      { type: 'tool-input-start', toolCallId: 'motion-tool', toolName: 'bash' },
      { type: 'tool-input-available', toolCallId: 'motion-tool', toolName: 'bash', input: { command: 'echo motion-probe' } },
      { type: 'tool-output-available', toolCallId: 'motion-tool', output: 'motion-probe' },
      { type: 'reasoning-start', id: 'motion-thinking' },
      { type: 'reasoning-delta', id: 'motion-thinking', delta: '**Motion evidence.**\n\n' + 'Long thinking remains readable while tools are toggled. '.repeat(180) },
    ]
    const stream = (route: import('@playwright/test').Route) => route.fulfill({
      headers: { 'content-type': 'text/event-stream', 'x-vercel-ai-ui-message-stream': 'v1', 'x-test-thinking-motion': 'true' },
      body: chunks.map(chunk => `data: ${JSON.stringify(chunk)}\n\n`).join(''),
    })
    // Resume one deterministic Native display stream without submitting commands.
    await page.route('**/chat/stream?*', stream)
    await page.route('**/chat/active?*', route => route.fulfill({ json: {
      active: true, status: 'running', phase: 'running', task_id: 'motion-task', active_operation_id: 'motion-run', stream_attached: true,
    } }))
    await page.goto('/')
    await openWritingAgent(page)
    const thinking = page.locator('[data-nova-scroll-lock="thinking"]').filter({ visible: true })
    await expect(thinking.locator('[data-streamdown="strong"]')).toHaveText('Motion evidence.')
    await expect(thinking.locator('[data-sd-animate]')).toHaveCount(0)
    const tool = page.locator('[data-nova-tool-header]').filter({ visible: true })
    await tool.click()
    await expect(tool).toHaveAttribute('aria-expanded', 'true')
    await tool.click()
    await expect(tool).toHaveAttribute('aria-expanded', 'false')
    await thinking.press('PageUp')

    const sidebar = page.getByLabel('工作台侧边栏')
    for (const [option, value] of [['完整', 'full'], ['跟随系统', 'system']] as const) {
      await sidebar.getByRole('button', { name: '设置', exact: true }).click()
      const row = page.locator('.nova-settings-row').filter({ hasText: '动效强度' })
      await row.getByRole('combobox').click()
      await page.getByRole('option', { name: option, exact: true }).click()
      await expect(page.locator('html')).toHaveAttribute('data-nova-motion', value)
      await openWritingAgent(page)
      await expect(thinking).toBeVisible()
      if (value === 'full') await expect.poll(() => thinking.locator('[data-sd-animate]').count()).toBeGreaterThan(0)
      else await expect(thinking.locator('[data-sd-animate]')).toHaveCount(0)
      await expect(thinking.locator('[data-streamdown="strong"]')).toHaveText('Motion evidence.')
    }
    await page.evaluate(() => window.dispatchEvent(new CustomEvent('test-thinking-motion', {
      detail: 'data: {"type":"reasoning-delta","id":"motion-thinking","delta":" Still streaming."}\n\n',
    })))
    await expect(thinking).toContainText('Still streaming.')
    await expect(thinking.locator('[data-sd-animate]')).toHaveCount(0)
    await expect(page.locator('html')).toHaveAttribute('data-theme', theme)
    await page.screenshot({ path: test.info().outputPath(`thinking-motion-${theme}.png`) })
  })
}
