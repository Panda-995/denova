import { createServer } from 'node:http'
import { expect, test } from '../support/fixtures'
import { createAndOpenBook } from '../support/api'

for (const theme of ['dark', 'light'] as const) {
  test(`search services persist and preview across writing and game (${theme})`, async ({ page, request, browserDiagnostics }, testInfo) => {
    browserDiagnostics.allow(/^console\.error: Failed to load resource:.*400.*\/api\/settings\/web-search\/test/)
    const server = createServer((req, res) => {
      const query = new URL(req.url!, 'http://localhost').searchParams.get('q')
      res.setHeader('Content-Type', 'application/json')
      res.end(JSON.stringify({ results: query === 'empty' ? [] : [{
        title: '创作资料与世界设定：' + '长标题检索结果'.repeat(12),
        url: 'https://example.com/research', content: '可用于核对创作资料的网页摘要。'.repeat(8),
      }] }))
    })
    await new Promise<void>(resolve => server.listen(0, '127.0.0.1', resolve))
    const address = server.address() as { port: number }
    try {
      await createAndOpenBook(request, 'Search Settings')
      const seeded = await request.patch('/api/settings', { data: { layer: 'user', changes: { theme, web_access: null } } })
      expect(seeded.ok()).toBe(true)
      await page.setViewportSize({ width: 1440, height: 1000 })
      await page.goto('/')
      const sidebar = page.getByLabel('工作台侧边栏')
      await sidebar.getByRole('button', { name: '设置', exact: true }).click()
      const editor = page.locator('[data-web-search-settings]')
      await editor.scrollIntoViewIfNeeded()
      await expect(editor.getByText('当前使用内置搜索。添加服务后，可将其设为默认搜索服务。')).toBeVisible()
      await page.screenshot({ path: testInfo.outputPath(`search-empty-${theme}.png`) })

      for (const service of ['DeepSeek', 'Perplexity', 'Tavily']) {
        await editor.getByRole('button', { name: '添加服务', exact: true }).click()
        await page.getByRole('menuitem', { name: service, exact: true }).click()
        await editor.getByLabel(`${service} API 密钥`, { exact: true }).fill(`${service}-test-key`)
        await expect(editor.getByLabel(`${service} API 密钥`, { exact: true })).toHaveAttribute('type', 'password')
        if (service !== 'DeepSeek') {
          await editor.getByRole('combobox', { name: '搜索模式', exact: true }).click()
          await page.getByRole('option', { name: service === 'Perplexity' ? '快速搜索' : '深入搜索', exact: true }).click()
        }
        // The whole service row toggles its contents, including its title.
        await editor.getByRole('button', { name: new RegExp(`服务 ${service} 已配置`) }).click()
        await expect(editor.getByLabel(`${service} API 密钥`, { exact: true })).toBeHidden()
      }
      const choose = async (name: string) => {
        await editor.getByRole('combobox', { name: '默认搜索服务', exact: true }).click()
        await page.getByRole('option', { name, exact: true }).click()
      }
      await expect(editor.getByRole('combobox', { name: '默认搜索服务' })).toHaveText('内置免费搜索')
      await choose('DeepSeek')
      await expect(editor.getByRole('button', { name: '移除 DeepSeek', exact: true })).toBeDisabled()
      await expect.poll(async () => (await (await request.get('/api/settings')).json()).user.web_access?.deepseek?.api_key).toBe('DeepSeek-test-key')
      await expect.poll(async () => (await (await request.get('/api/settings')).json()).user.web_access?.search_provider).toBe('deepseek')
      await expect.poll(async () => (await (await request.get('/api/settings')).json()).user.web_access?.perplexity?.mode).toBe('fast')
      await expect.poll(async () => (await (await request.get('/api/settings')).json()).user.web_access?.tavily?.mode).toBe('advanced')
      for (const destination of ['写作', '游戏']) {
        await sidebar.getByRole('button', { name: destination, exact: true }).click()
        await expect(editor).toBeHidden()
        await sidebar.getByRole('button', { name: '设置', exact: true }).click()
        await expect(editor.getByRole('combobox', { name: '默认搜索服务' })).toHaveText('DeepSeek')
      }

      await page.reload()
      await expect(editor).toBeVisible()
      await editor.getByRole('button', { name: /默认 DeepSeek 已配置/ }).click()
      const key = editor.getByLabel('DeepSeek API 密钥', { exact: true })
      await expect(key).toHaveValue('DeepSeek-test-key')
      await key.fill('')
      await editor.getByLabel('测试搜索', { exact: true }).fill('creator tools')
      await editor.getByRole('button', { name: '搜索测试', exact: true }).click()
      await expect(editor.getByRole('alert')).toContainText('请填写所选搜索服务')
      await key.fill('DeepSeek-test-key')
      await expect(editor.getByRole('alert')).toHaveCount(0)
      await editor.getByRole('button', { name: /默认 DeepSeek 已配置/ }).click()

      await editor.getByRole('button', { name: '添加服务', exact: true }).click()
      await page.getByRole('menuitem', { name: 'SearXNG', exact: true }).click()
      await editor.getByLabel('SearXNG 实例地址', { exact: true }).fill(`http://127.0.0.1:${address.port}`)
      await choose('SearXNG')
      await editor.getByRole('button', { name: /默认 SearXNG 已配置/ }).click()
      await editor.getByRole('button', { name: '搜索测试', exact: true }).click()
      await expect(editor.getByRole('link', { name: /^创作资料与世界设定/ })).toHaveAttribute('href', 'https://example.com/research')
      await expect(editor.getByText(/SearXNG · 1 条结果/)).toBeVisible()
      await editor.scrollIntoViewIfNeeded()
      await page.screenshot({ path: testInfo.outputPath(`search-services-${theme}.png`) })

      await page.setViewportSize({ width: 390, height: 844 })
      await editor.getByLabel('测试搜索', { exact: true }).scrollIntoViewIfNeeded()
      await expect(editor.getByRole('link', { name: /^创作资料与世界设定/ })).toBeVisible()
      expect(await editor.evaluate(element => element.scrollWidth <= element.clientWidth)).toBe(true)
      await page.screenshot({ path: testInfo.outputPath(`search-narrow-${theme}.png`) })
      await editor.getByLabel('测试搜索', { exact: true }).fill('empty')
      await expect(editor.getByRole('link', { name: /^创作资料与世界设定/ })).toHaveCount(0)
      await editor.getByRole('button', { name: '搜索测试', exact: true }).click()
      await expect(editor.getByText('SearXNG 连接成功，但没有搜索结果。请调整关键词。')).toBeVisible()

      await page.setViewportSize({ width: 1440, height: 1000 })
      await editor.getByRole('button', { name: '移除 Perplexity', exact: true }).click()
      await expect.poll(async () => (await (await request.get('/api/settings')).json()).user.web_access?.perplexity).toBeUndefined()
      await editor.getByRole('button', { name: /高级 搜索与网页读取选项/ }).click()
      await expect(editor.getByRole('combobox', { name: '搜索失败时' })).toHaveText('直接报告错误')
      await expect(editor.getByRole('spinbutton', { name: '网页响应上限 (KB)', exact: true })).toBeVisible()
      if (theme === 'light') {
        expect((await request.patch('/api/settings', { data: { layer: 'user', changes: { language: 'en-US' } } })).ok()).toBe(true)
        await page.reload()
        await editor.scrollIntoViewIfNeeded()
        await expect(editor.getByRole('combobox', { name: 'Default search service', exact: true })).toBeVisible()
        await expect(editor.getByRole('button', { name: 'Run search test', exact: true })).toBeVisible()
        expect(await editor.innerText()).not.toMatch(/[\u4e00-\u9fff]/)
        await page.screenshot({ path: testInfo.outputPath('search-english-light.png') })
      }
    } finally {
      await new Promise<void>((resolve, reject) => server.close(error => error ? reject(error) : resolve()))
    }
  })
}
