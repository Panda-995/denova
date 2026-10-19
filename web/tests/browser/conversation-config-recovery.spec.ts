import { mkdtemp } from 'node:fs/promises'
import path from 'node:path'
import { runtimeRoot } from '../../scripts/e2e-paths.mjs'
import { expect, test } from '../support/fixtures'
import { createAgentChatSession, createAndOpenBook, createStartedStory, registerAgentChatProject } from '../support/api'
import { openAgentChatSession, openAgentChatWorkbench, openWritingAgent } from '../support/agent-chat'

for (const surface of ['writing', 'general', 'game'] as const) {
  for (const theme of ['dark', 'light'] as const) {
    test(`${surface} preserves drafts and exposes configuration recovery in ${theme}`, async ({ page, request, browserDiagnostics }) => {
      const settings = await (await request.get('/api/settings')).json()
      expect((await request.patch('/api/settings', { data: {
        layer: 'user', base_revision: settings.revisions.user, changes: { theme },
      } })).ok()).toBe(true)
      let projectId = (await createAndOpenBook(request, `Configuration recovery ${surface}`)).projectId
      if (surface === 'game') await createStartedStory(request, 'Recover configuration')
      if (surface === 'general') {
        projectId = (await registerAgentChatProject(request, await mkdtemp(path.join(runtimeRoot, 'config-recovery-')))).id
        await createAgentChatSession(request, projectId, 'Recover configuration')
      }
      const configPath = `/api/projects/${projectId}/conversation-config`
      let failing = true
      browserDiagnostics.allow(/^console\.error: Failed to load resource:.*400.*\/conversation-config/)
      await page.route(`**${configPath}**`, route => failing && route.request().method() === 'GET'
        ? route.fulfill({ status: 400, json: { error: '保存的模型配置不可用，请修复后重试。', code: 'api.common.invalidRequest' } })
        : route.continue())
      await page.goto('/')
      if (surface === 'writing') await openWritingAgent(page)
      else if (surface === 'general') {
        await openAgentChatWorkbench(page)
        await openAgentChatSession(page, projectId, 'Recover configuration')
      } else await page.getByLabel('工作台侧边栏').getByRole('button', { name: '游戏', exact: true }).click()
      const editor = page.getByPlaceholder(surface === 'game' ? /你要做什么/ : /输入消息/).filter({ visible: true })
      const error = page.getByRole('alert').filter({ hasText: '保存的模型配置不可用' })
      await expect(error).toBeVisible()
      await expect(editor).toHaveAttribute('contenteditable', 'true')
      await editor.fill('保留草稿 / Keep my draft')
      await expect(page.getByRole('button', { name: '发送', exact: true }).filter({ visible: true })).toBeDisabled()
      await expect(page.getByRole('button', { name: '配置', exact: true }).filter({ visible: true })).toBeEnabled()
      await page.screenshot({ path: test.info().outputPath(`${surface}-${theme}-configuration-error.png`) })

      await page.setViewportSize({ width: 390, height: 900 })
      if (surface === 'writing') await page.getByRole('tab', { name: 'Agent', exact: true }).click()
      await expect(error).toBeVisible()
      await expect(editor).toHaveText('保留草稿 / Keep my draft')
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
      await page.screenshot({ path: test.info().outputPath(`${surface}-${theme}-configuration-error-narrow.png`) })

      failing = false
      await page.getByRole('button', { name: '重试', exact: true }).filter({ visible: true }).click()
      await expect(error).toHaveCount(0)
      await expect(editor).toHaveText('保留草稿 / Keep my draft')
      await expect(page.getByRole('button', { name: '发送', exact: true }).filter({ visible: true })).toBeEnabled()
      await page.getByRole('button', { name: /切换模型/ }).filter({ visible: true }).click()
      await expect(page.getByRole('menuitem', { name: /E2E deterministic model/ }).first()).toBeEnabled()
    })
  }
}
