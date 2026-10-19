import { randomUUID } from 'node:crypto'
import { mkdtemp } from 'node:fs/promises'
import path from 'node:path'
import { runtimeRoot } from '../../scripts/e2e-paths.mjs'
import { expect, test } from '../support/fixtures'
import { createAgentChatSession, createAndOpenBook, createStartedStory, registerAgentChatProject } from '../support/api'
import { openAgentChatSession, openAgentChatWorkbench, openWritingAgent, submitAgentChatMessage } from '../support/agent-chat'
import { getModelStatus, releaseDelayedRequest } from '../support/model'

for (const product of ['general', 'writing', 'game'] as const) {
  test(`${product} removes old queued instructions while idle and during a new task`, async ({ page, request }) => {
    page.setDefaultTimeout(10_000)
    const settings = await (await request.get('/api/settings')).json()
    const theme = product === 'writing' ? 'light' : 'dark'
    await page.route(/\/api\/(?:projects\/[^/]+\/)?settings$/, route => route.fulfill({ json: {
      ...settings, effective: { ...settings.effective, theme, language: 'zh-CN' },
    } }))
    let projectId = ''
    let sessionTitle = ''
    if (product === 'general') {
      const project = await registerAgentChatProject(request, await mkdtemp(path.join(runtimeRoot, 'old-queue-')))
      projectId = project.id
      sessionTitle = (await createAgentChatSession(request, project.id, 'Old queued instructions')).title
    } else {
      projectId = (await createAndOpenBook(request, `Old queue ${product}`)).projectId
      if (product === 'game') await createStartedStory(request, 'Old queued game instructions')
    }
    const marker = product === 'game' ? 'E2E_GAME_FOLLOW_UP_DELAY' : 'E2E_DELAYED_AGENT_REPLY'
    let activeURL = ''
    page.on('request', value => {
      const url = new URL(value.url())
      if (value.method() === 'GET' && url.pathname === (product === 'game'
        ? '/api/interactive/chat/active' : `/api/projects/${projectId}/agent-chat/chat/active`)) activeURL = value.url()
    })
    await page.goto('/')
    if (product === 'general') await openAgentChatWorkbench(page)
    else if (product === 'game') await page.getByLabel('工作台侧边栏').getByRole('button', { name: '游戏', exact: true }).click()
    let composer = product === 'general' ? await openAgentChatSession(page, projectId, sessionTitle)
      : product === 'writing' ? await openWritingAgent(page) : page.getByPlaceholder(/你要做什么/)
    const readActive = async () => (await request.get(activeURL)).json()
    try {
      await submitAgentChatMessage(page, composer, `Keep task A waiting. ${marker}`)
      await expect.poll(async () => (await getModelStatus(request)).delayed_waiting_by_marker[marker] ?? 0).toBe(1)
      await expect.poll(() => activeURL).not.toBe('')
      await expect.poll(async () => (await readActive()).phase).toBe('running')
      const taskA = await readActive()
      expect(taskA.active_operation_id).not.toBe('')
      await expect(page.locator('[data-action="stop"]').filter({ visible: true })).toBeEnabled()
      await expect(composer).toHaveText('')
      const queue = page.getByRole('region', { name: '排队中的指令' }).filter({ visible: true })
      for (const [index, text] of ['Remove while idle', 'Remove during task B. '.repeat(20), 'Return this draft'].entries()) {
        await submitAgentChatMessage(page, composer, text)
        await expect(queue.getByRole('button', { name: '删除排队指令', exact: true })).toHaveCount(index + 1)
        await expect(composer).toHaveText('')
      }
      await expect(queue.getByRole('button', { name: '删除排队指令', exact: true })).toHaveCount(3)
      await page.locator('[data-action="stop"]').filter({ visible: true }).click()
      await expect(page.getByRole('button', { name: '取消任务', exact: true })).toBeVisible()
      await page.getByRole('button', { name: '取消任务', exact: true }).click()
      await expect.poll(async () => (await readActive()).phase).toBe('idle')
      await expect(page.locator('[data-action="stop"]').filter({ visible: true })).toHaveCount(0)
      await releaseDelayedRequest(request, marker)
      await page.reload()
      if (product === 'general') {
        await openAgentChatWorkbench(page)
        composer = await openAgentChatSession(page, projectId, sessionTitle)
      } else if (product === 'writing') composer = await openWritingAgent(page)
      else await page.getByLabel('工作台侧边栏').getByRole('button', { name: '游戏', exact: true }).click()

      await expect(queue.getByRole('button', { name: '立即转向', exact: true }).first()).toBeDisabled()
      await queue.getByRole('button', { name: '删除排队指令', exact: true }).first().click()
      await expect.poll(async () => (await readActive()).queue?.length).toBe(2)
      await submitAgentChatMessage(page, composer, `Keep task B waiting. ${marker}`)
      await expect.poll(async () => (await getModelStatus(request)).delayed_waiting_by_marker[marker] ?? 0).toBe(1)
      await expect.poll(async () => {
        const active = await readActive()
        return active.phase === 'running' && active.active_operation_id !== taskA.active_operation_id
      }).toBe(true)
      const taskB = await readActive()
      expect(taskB.active_operation_id).not.toBe(taskA.active_operation_id)
      expect(taskB.queue.every((item: { operation_id: string }) => item.operation_id === taskA.active_operation_id)).toBe(true)
      await expect(queue.getByRole('button', { name: '立即转向', exact: true }).first()).toBeDisabled()

      for (const width of [1440, 390]) {
        await page.setViewportSize({ width, height: 960 })
        if (product === 'writing' && width === 390) await page.getByRole('tab', { name: 'Agent', exact: true }).click()
        await expect(queue).toBeVisible()
        await expect(queue.getByRole('button', { name: '删除排队指令', exact: true }).first()).toBeEnabled()
        expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
        await page.screenshot({ path: test.info().outputPath(`${product}-${theme}-${width}.png`) })
      }
      composer = page.getByPlaceholder(product === 'game' ? /你要做什么/ : /输入消息/).filter({ visible: true })

      // A stale projection cannot steer an earlier task through the API either.
      const commandsURL = activeURL.replace('/active', '/commands')
      const staleSteer = await request.post(commandsURL, { data: {
        type: 'steer_queued', command_id: randomUUID(), target_operation_id: taskA.active_operation_id,
        target_command_id: taskB.queue[0].command_id,
        ...(product === 'game' ? { story_id: taskB.story_id || new URL(activeURL).searchParams.get('story_id'), branch_id: 'main' }
          : { session_id: new URL(activeURL).searchParams.get('session_id') }),
      } })
      expect(staleSteer.status()).toBe(409)
      await queue.getByRole('button', { name: '删除排队指令', exact: true }).first().click()
      await expect.poll(async () => (await readActive()).queue?.length).toBe(1)
      if (product === 'game') await queue.getByRole('button', { name: '删除排队指令', exact: true }).click()
      else {
        await queue.getByRole('button', { name: '更多排队指令操作', exact: true }).click()
        await page.getByRole('menuitem', { name: '返回编辑', exact: true }).click()
        await expect(composer).toHaveText('Return this draft')
      }
      await expect.poll(async () => (await readActive()).queue?.length).toBe(0)
      expect((await readActive()).active_operation_id).toBe(taskB.active_operation_id)
      await expect(page.locator('[data-action="stop"]').filter({ visible: true })).toBeEnabled()
      const pause = page.waitForRequest(value => value.method() === 'POST' && value.postDataJSON()?.type === 'suspend')
      await page.locator('[data-action="stop"]').filter({ visible: true }).click()
      expect((await pause).postDataJSON().target_operation_id).toBe(taskB.active_operation_id)
      await expect.poll(async () => (await readActive()).phase).toBe('suspended')
      await page.getByRole('button', { name: '取消任务', exact: true }).click()
      await releaseDelayedRequest(request, marker)
      await expect.poll(async () => (await readActive()).phase).toBe('idle')
    } finally {
      await releaseDelayedRequest(request, marker)
    }
  })
}
