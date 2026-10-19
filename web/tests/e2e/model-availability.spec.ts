import { expect, test } from '../support/fixtures'
import { createAndOpenBook, createStartedStory } from '../support/api'
import { openWritingAgent } from '../support/agent-chat'

for (const theme of ['dark', 'light'] as const) {
  test(theme === 'light' ? 'renders long model labels across Writing, Game and settings in light' : 'connection and model switches preserve configuration and filter Writing and Game choices in dark', async ({ page, request }) => {
    await createAndOpenBook(request, `Model availability ${theme}`)
    await createStartedStory(request, `Model availability ${theme}`)
    const created = await request.post('/api/sessions', { data: { title: 'Model availability' } })
    expect(created.ok(), await created.text()).toBe(true)
    const session = await created.json()
    expect((await request.post('/api/sessions/switch', { data: { id: session.id } })).ok()).toBe(true)
    const original = await (await request.get('/api/settings')).json()
    const connectionName = `Optional connection ${'long name '.repeat(8)}`.trim()
    const modelName = `Optional model ${'long name '.repeat(8)}`.trim()
    const seeded = await request.patch('/api/settings', { data: {
      layer: 'user', base_revision: original.revisions.user, changes: {
        theme, language: 'zh-CN',
        model_endpoints: [...original.user.model_endpoints, {
          id: 'optional', name: connectionName, provider: 'openai-compatible',
          protocol: 'openai-chat-completions', base_url: 'http://127.0.0.1:1/v1', api_key: 'fixture-only',
        }],
        model_profiles: [...original.user.model_profiles,
          { id: 'default', name: modelName, endpoint_id: 'optional', model: 'optional-a' },
          { id: 'optional-b', endpoint_id: 'optional', model: 'optional-b' },
        ],
      },
    } })
    expect(seeded.ok(), await seeded.text()).toBe(true)
    await page.addInitScript(() => localStorage.setItem('nova:onboarding:v1', JSON.stringify({ version: 1, skipped: true })))
    await page.goto('/')
    const sidebar = page.getByLabel('工作台侧边栏')
    const connection = page.locator('[data-settings-level="connection"]').filter({ has: page.getByRole('switch', { name: `启用连接：${connectionName}`, exact: true }) })
    const connectionSwitch = connection.getByRole('switch', { name: `启用连接：${connectionName}`, exact: true })
    const modelSwitch = connection.getByRole('switch', { name: `启用模型：${modelName}`, exact: true })

    const openSettings = async () => {
      const settingsButton = sidebar.getByRole('button', { name: '设置', exact: true })
      await expect(settingsButton).toBeVisible()
      if (await settingsButton.getAttribute('aria-current') !== 'page') await settingsButton.click()
      await page.locator('.nova-settings-view nav').getByRole('button', { name: '语言模型', exact: true }).click()
      const trigger = connection.getByRole('button').first()
      if (await trigger.getAttribute('aria-expanded') === 'false') await trigger.click()
    }
    const expectChoices = async (a: boolean, b: boolean) => {
      for (const surface of ['writing', 'game']) {
        if (surface === 'writing') await expect(await openWritingAgent(page)).toBeInViewport()
        else {
          await sidebar.getByRole('button', { name: '游戏', exact: true }).click()
          await expect(page.getByPlaceholder(/你要做什么/).filter({ visible: true })).toBeInViewport()
        }
        const modelPicker = page.locator('[data-model-profile-trigger]').filter({ visible: true })
        await expect(modelPicker).toBeEnabled()
        await expect(modelPicker).toBeInViewport()
        await modelPicker.click()
        await expect(page.getByRole('menuitem', { name: `默认：${modelName}`, exact: true })).toHaveCount(a ? 1 : 0)
        await expect(page.getByRole('menuitem', { name: 'optional-b（optional-b）', exact: true })).toHaveCount(b ? 1 : 0)
        await page.keyboard.press('Escape')
        await expect(page.getByRole('menu')).toBeHidden()
      }
    }
    const expectSaved = async (endpointEnabled: boolean | undefined, modelEnabled: boolean | undefined) => {
      await expect.poll(async () => {
        const settings = (await (await request.get('/api/settings')).json()).user
        return { endpoint: settings.model_endpoints.find((item: { id: string }) => item.id === 'optional')?.enabled,
          model: settings.model_profiles.find((item: { id: string }) => item.id === 'default')?.enabled }
      }).toEqual({ endpoint: endpointEnabled, model: modelEnabled })
    }

    await expectChoices(true, true)
    // Long connection/model labels render in both themes and widths. The
    // enable/disable state machine and reload recovery run once in dark.
    if (theme === 'light') {
      await openSettings()
      for (const width of [1440, 390]) {
        await page.setViewportSize({ width, height: 900 })
        await connectionSwitch.scrollIntoViewIfNeeded()
        await expect(connectionSwitch).toBeVisible()
        expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
        await page.screenshot({ path: test.info().outputPath(`model-availability-${theme}-${width}.png`) })
      }
      return
    }
    await openSettings()
    await expect(connectionSwitch).toBeChecked()
    await expect(modelSwitch).toBeChecked()
    await modelSwitch.click()
    await expectSaved(undefined, false)
    // Switching availability must not also toggle the disclosure row.
    await expect(connection.getByRole('button').first()).toHaveAttribute('aria-expanded', 'true')
    await page.reload()
    await openSettings()
    await expect(modelSwitch).not.toBeChecked()
    await page.getByRole('combobox', { name: '默认语言模型', exact: true }).click()
    await expect(page.getByRole('option', { name: `${modelName}（已关闭）`, exact: true })).toHaveAttribute('data-disabled', '')
    await page.keyboard.press('Escape')
    await expect(page.getByRole('listbox')).toBeHidden()
    await expectChoices(false, true)
    await openSettings()
    await connectionSwitch.click()
    await expectSaved(false, false)
    await page.reload()
    await openSettings()
    await expect(connectionSwitch).not.toBeChecked()
    await expect(connection.getByRole('switch', { name: '启用模型：optional-b', exact: true })).toBeChecked()
    for (const width of [1440, 390]) {
      await page.setViewportSize({ width, height: 900 })
      for (const [kind, control, remove] of [
        ['connection', connectionSwitch, connection.getByRole('button', { name: '删除连接', exact: true })],
        ['model', modelSwitch, connection.locator('[data-settings-level="model"]').first().getByRole('button', { name: '删除语言模型', exact: true })],
      ] as const) {
        await control.scrollIntoViewIfNeeded()
        await expect(control).toBeInViewport()
        const switchBounds = (await control.boundingBox())!
        const deleteBounds = (await remove.boundingBox())!
        expect(switchBounds.x + switchBounds.width).toBeLessThanOrEqual(deleteBounds.x)
        expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
        await page.screenshot({ path: test.info().outputPath(`${kind}-switch-${theme}-${width}.png`) })
      }
    }
    await page.setViewportSize({ width: 1440, height: 900 })
    await expectChoices(false, false)
    await openSettings()
    await connectionSwitch.click()
    await expectSaved(true, false)
    await expectChoices(false, true)
    await openSettings()
    await modelSwitch.click()
    await expectSaved(true, true)
    await expectChoices(true, true)
    const restored = (await (await request.get('/api/settings')).json()).user
    expect(restored.model_endpoints.find((item: { id: string }) => item.id === 'optional')).toMatchObject({ name: connectionName, base_url: 'http://127.0.0.1:1/v1' })
    expect(restored.model_profiles.filter((item: { endpoint_id: string }) => item.endpoint_id === 'optional')).toEqual([
      { id: 'default', name: modelName, endpoint_id: 'optional', model: 'optional-a', enabled: true },
      { id: 'optional-b', endpoint_id: 'optional', model: 'optional-b' },
    ])
    const localized = await request.patch('/api/settings', { data: { layer: 'user', changes: { language: 'en-US' } } })
    expect(localized.ok(), await localized.text()).toBe(true)
    await page.reload()
    await page.getByLabel('Workbench sidebar').getByRole('button', { name: 'Settings', exact: true }).click()
    await page.locator('.nova-settings-view nav').getByRole('button', { name: 'Language Models', exact: true }).click()
    await expect(page.getByRole('switch', { name: `Enable connection: ${connectionName}`, exact: true })).toBeChecked()
  })
}
