import { expect, test } from '../support/fixtures'
import { createAndOpenBook, createProjectFile, readProjectFile } from '../support/api'
import { openWritingAgent, submitAgentChatMessage } from '../support/agent-chat'
import { getModelStatus, modelControlURL, releaseDelayedRequest } from '../support/model'

const delayedReplyMarker = 'E2E_DELAYED_AGENT_REPLY'

test('applies an Agent edit and supports review, undo, and redo', { tag: '@smoke' }, async ({ page, request }) => {
  const book = await createAndOpenBook(request, 'Writing Agent E2E Book')
  const chapterPath = 'chapters/e2e-agent-chapter.md'
  const original = '# Agent 测试\n\nAgent 修改前。'
  await createProjectFile(request, book.projectId, chapterPath, original)

  await page.goto('/')
  await page.getByLabel('工作台侧边栏').getByRole('button', { name: '写作', exact: true }).click()
  await page.getByRole('button', { name: /^e2e agent chapter\b/ }).click()
  const composer = await openWritingAgent(page)
  await submitAgentChatMessage(page, composer, 'Update the chapter for the deterministic scenario. E2E_EDIT_CHAPTER')

  const summary = page.getByRole('region', { name: '已编辑 1 个文件' })
  await expect(summary).toBeVisible()
  await expect.poll(async () => (await readProjectFile(request, book.projectId, chapterPath)).content)
    .toContain('Agent 已通过工具完成修改。')

  await summary.getByRole('button', { name: '审阅', exact: true }).click()
  const review = page.getByRole('region', { name: '变更审阅' })
  await expect(review).toBeVisible()
  await review.getByRole('button', { name: '接受本轮', exact: true }).click()

  const undo = review.getByRole('button', { name: '撤销整组', exact: true })
  await expect(undo).toBeEnabled()
  await undo.click()
  await expect.poll(async () => (await readProjectFile(request, book.projectId, chapterPath)).content).toBe(original)

  const redo = review.getByRole('button', { name: '重做整组', exact: true })
  await expect(redo).toBeEnabled()
  await redo.click()
  await expect.poll(async () => (await readProjectFile(request, book.projectId, chapterPath)).content)
    .toContain('Agent 已通过工具完成修改。')
})

test('reattaches to an Agent run after the page reloads mid-response', async ({ page, request }) => {
  await createAndOpenBook(request, 'Agent Recovery E2E Book')
  await page.goto('/')
  const composer = await openWritingAgent(page)
  await submitAgentChatMessage(page, composer, 'Wait until the browser reconnects. E2E_DELAYED_AGENT_REPLY')

  await expect.poll(async () => (await getModelStatus(request)).delayed_waiting_by_marker[delayedReplyMarker] ?? 0)
    .toBe(1)

  let released = false
  try {
    await page.reload()
    await releaseDelayedRequest(request, delayedReplyMarker)
    released = true
  } finally {
    if (!released) await releaseDelayedRequest(request, delayedReplyMarker)
  }

  await openWritingAgent(page)
  await expect(page.getByText('Recovered response completed exactly once.', { exact: true })).toHaveCount(1)
})

test('opts into Native Todo using the existing switch and keeps history after disabling', async ({ page, request }) => {
  await createAndOpenBook(request, 'Writing Todo preference')
  const marker = 'E2E_RUNTIME_TODO_WRITING'
  const settings = await (await request.get('/api/settings')).json()
  const selected = await request.patch('/api/settings', { data: {
    layer: 'user', base_revision: settings.revisions.user, changes: { agent_runtimes: { ide: { selected: 'native' } } },
  } })
  expect(selected.ok(), await selected.text()).toBe(true)
  const wire = async (): Promise<Array<{ tools: Array<{ function: { name: string } }> }>> =>
    (await request.get(`${modelControlURL}/control/runtime-requests?marker=${marker}`)).json()
  const expectSavedList = async () => {
    const execution = page.getByRole('button', { name: /^执行过程 · 1 / })
    if (await execution.getAttribute('aria-expanded') === 'false') await execution.click()
    await expect(page.locator('li').getByText('Verify the saved writing draft', { exact: true })).toBeVisible()
  }

  await page.goto('/')
  for (const stage of ['DEFAULT', 'ENABLED', 'DISABLED']) {
    const enabled = stage === 'ENABLED'
    if (stage !== 'DEFAULT') {
      await page.getByRole('button', { name: 'Agents', exact: true }).click()
      const capabilities = page.locator('[data-agent-configuration-section="capabilities"]')
      const toggle = capabilities.getByRole('switch', { name: '任务清单', exact: true })
      if (!await toggle.isVisible()) await capabilities.getByRole('button', { name: '能力', exact: true }).click()
      await expect(toggle).toBeChecked({ checked: !enabled })
      await toggle.setChecked(enabled)
      await expect.poll(async () => (await (await request.get('/api/settings')).json()).user.agent_tools?.ide?.todo).toBe(enabled)
      await page.reload()
    }
    const before = (await wire()).length
    await submitAgentChatMessage(page, await openWritingAgent(page), `${marker} STAGE_${stage}`)
    await expect(page.getByText(`${marker} ${stage} complete.`, { exact: true })).toBeVisible()
    const calls = (await wire()).slice(before)
    expect(calls.length).toBeGreaterThan(0)
    for (const call of calls) expect(call.tools.some(tool => tool.function.name === 'todo')).toBe(enabled)
    if (stage !== 'DEFAULT') await expectSavedList()
  }

  await page.reload()
  await openWritingAgent(page)
  await expectSavedList()
})
