import { expect, test } from '../support/fixtures'
import { createAndOpenBook, createStartedStory } from '../support/api'
import { openWritingAgent, submitAgentChatMessage } from '../support/agent-chat'
import { createRequire } from 'node:module'
import { unlink, writeFile } from 'node:fs/promises'
import path from 'node:path'

test('uses native Gemini tools, image results and restored signatures in Writing and Game', async ({ page, request }, testInfo) => {
  const modelBase = `http://127.0.0.1:${process.env.DENOVA_E2E_MODEL_PORT || '18081'}`
  const current = await (await request.get('/api/settings')).json()
  const saved = await request.patch('/api/settings', { data: { layer: 'user', base_revision: current.revisions.user, changes: {
    model_endpoints: [...current.user.model_endpoints, { id: 'native-gemini', name: 'Native Gemini', provider: 'google', protocol: 'google-generative-ai', base_url: modelBase, api_key: 'test-key' }],
    model_profiles: [...current.user.model_profiles, { id: 'native-gemini', name: 'Native Gemini', endpoint_id: 'native-gemini', model: 'gemini-e2e', context_window_tokens: 100000 }],
    agent_models: Object.fromEntries(['default', 'ide', 'interactive_story'].map(kind => [kind, { profile_id: 'native-gemini', thinking_level: 'default' }])),
  } } })
  expect(saved.ok(), await saved.text()).toBe(true)
  const book = await createAndOpenBook(request, 'Native Gemini Book')
  await createStartedStory(request, 'Native Gemini Story')
  const discovery = await request.post('/api/models/discover', { data: {
    endpoint: { id: 'native-gemini', provider: 'google', protocol: 'google-generative-ai', base_url: modelBase, api_key: 'test-key' },
    profile: { id: 'native-gemini', endpoint_id: 'native-gemini' },
  } })
  expect(discovery.ok(), await discovery.text()).toBe(true)
  expect((await discovery.json()).models).toMatchObject([{ id: 'gemini-e2e' }])

  const sharp = createRequire(import.meta.url)('sharp') as typeof import('sharp').default
  const image = await sharp({ create: { width: 96, height: 64, channels: 3, background: '#165cbd' } }).png().toBuffer()
  const source = path.join(book.workspace, 'e2e-tool-image.png')
  await page.goto('/')
  const sidebar = page.getByLabel('工作台侧边栏')
  await sidebar.getByRole('button', { name: '设置', exact: true }).click()
  await page.locator('.nova-settings-view nav').getByRole('button', { name: '语言模型', exact: true }).click()
  await page.getByRole('button', { name: /连接 \d+ Native Gemini/ }).click()
  await expect(page.getByRole('combobox').filter({ hasText: 'Gemini 原生协议' })).toBeVisible()
  await page.getByRole('button', { name: '选择模型', exact: true }).last().click()
  await expect(page.getByRole('option', { name: /Gemini E2E/ })).toBeVisible()
  await page.keyboard.press('Escape')
  await page.screenshot({ path: testInfo.outputPath('native-gemini-settings.png') })

  let nativeRequests = (await (await request.get(`${modelBase}/control/gemini-requests`)).json()).length
  for (const surface of ['writing', 'game'] as const) {
    await writeFile(source, image)
    expect((await request.patch('/api/settings', { data: { layer: 'user', changes: { theme: surface === 'writing' ? 'dark' : 'light' } } })).ok()).toBe(true)
    await page.reload()
    const open = async () => {
      if (surface === 'writing') return openWritingAgent(page)
      await sidebar.getByRole('button', { name: '游戏', exact: true }).click()
      const composer = page.getByPlaceholder(/你要做什么/)
      await expect(composer).toBeVisible()
      return composer
    }
    let composer = await open()
    const reply = surface === 'writing' ? 'Tool image reached the model.' : '工具读取的图片已呈现，旧车站地图上的路线清晰可见。'
    await submitAgentChatMessage(page, composer, 'Read e2e-tool-image.png and describe it. E2E_TOOL_IMAGE_READ')
    await expect(page.getByText(reply, { exact: true })).toHaveCount(1)
    await expect(page.locator('[data-action="stop"]').filter({ visible: true })).toHaveCount(0)
    await unlink(source)
    await page.reload()
    composer = await open()
    await submitAgentChatMessage(page, composer, 'Continue using the captured pixels. E2E_TOOL_IMAGE_READ')
    await expect(page.getByText(reply, { exact: true })).toHaveCount(2)
    await expect(page.locator('[data-action="stop"]').filter({ visible: true })).toHaveCount(0)
    await page.screenshot({ path: testInfo.outputPath(`${surface}-native-gemini.png`) })
    const captured = await (await request.get(`${modelBase}/control/gemini-requests`)).json()
    expect(captured.length, `${surface} must use native Gemini requests`).toBeGreaterThan(nativeRequests)
    expect(captured.slice(nativeRequests).some((body: { contents?: Array<{ parts: Array<{ functionResponse?: { parts?: unknown[] } }> }> }) => body.contents?.some(content => content.parts.some(part => part.functionResponse?.parts?.length)))).toBe(true)
    nativeRequests = captured.length
  }
})
