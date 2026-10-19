import { readFile } from 'node:fs/promises'
import { expect, test } from '../support/fixtures'
import { createAndOpenBook, createAgentChatSession } from '../support/api'
import { openAgentChatWorkbench } from '../support/agent-chat'
import { getModelStatus, releaseDelayedRequest } from '../support/model'

test('searches saved conversation bodies and downloads the full visible Markdown transcript', async ({ page, request }) => {
  test.setTimeout(180_000)
  const book = await createAndOpenBook(request, 'Search and export')
  const title = '历史会话：很长的标题，用于确认摘要不会挤出时间和更多操作，也不会撑开窄屏布局'
  const session = await createAgentChatSession(request, book.projectId, title)
  const apiPath = `/api/projects/${book.projectId}/agent-chat`
  const firstMessage = `${'这是一段需要保留的创作背景。'.repeat(14)}灯塔档案 BeaconArchive\n\n\`\`\`go\nfmt.Println("hello")\n\`\`\`\nE2E_GENERAL_PROJECT_ALPHA_WRITE`
  // More than one export page and the UI's recent window. Use actual committed
  // turns so the test also catches canonical/display duplicate projections.
  const turns = 32
  for (let turn = 0; turn < turns; turn++) {
    const response = await request.post(`${apiPath}/chat`, { data: {
      command_id: `archive-${session.id}-${turn}`, session_id: session.id,
      message: turn === 0 ? firstMessage : `Saved turn ${turn}`,
    } })
    expect(response.ok(), await response.text()).toBe(true)
  }
  const recent = await (await request.get(`${apiPath}/session/messages?session_id=${session.id}&limit=100`)).json()
  expect(recent.page.has_more).toBe(true)
  expect(JSON.stringify(recent.messages)).not.toContain('BeaconArchive')

  const titleMatch = await createAgentChatSession(request, book.projectId, 'BeaconArchive title match')
  const history = await (await request.get('/api/agent-chat/history', { params: {
    project_id: book.projectId, query: 'beaconarchive', limit: 1,
  } })).json()
  expect(history).toMatchObject({ total: 2, has_more: true, items: [{ session: { id: titleMatch.id } }] })
  expect(history.items[0].excerpt).toBeUndefined()
  const older = await (await request.get('/api/agent-chat/history', { params: {
    project_id: book.projectId, query: 'beaconarchive', offset: 1, limit: 1,
  } })).json()
  expect(older.items[0]).toMatchObject({ session: { id: session.id }, excerpt: expect.stringContaining('BeaconArchive') })
  expect(older.items[0].excerpt.startsWith('…')).toBe(true)
  const toolOnly = await (await request.get('/api/agent-chat/history', { params: {
    project_id: book.projectId, query: 'e2e-project-proof.txt',
  } })).json()
  expect(toolOnly.total).toBe(0)

  await page.goto('/')
  await openAgentChatWorkbench(page)
  await page.getByRole('button', { name: '对话历史', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: '对话历史', exact: true })
  const search = dialog.getByRole('textbox', { name: '搜索对话历史' })
  await search.fill('beaconarchive')
  await expect(dialog.getByRole('button', { name: `打开 ${title}`, exact: true })).toBeVisible()
  await expect(dialog.locator('mark')).toHaveText('BeaconArchive')
  await expect(dialog.locator('mark')).toBeInViewport({ ratio: 1 })
  await page.screenshot({ path: test.info().outputPath('history-dark-wide.png'), animations: 'disabled' })
  await dialog.getByRole('button', { name: `${title} 的操作`, exact: true }).click()
  const downloaded = page.waitForEvent('download')
  await page.getByRole('menuitem', { name: '导出 Markdown', exact: true }).click()
  const download = await downloaded
  expect(download.suggestedFilename()).toBe(`${session.id}.md`)
  const markdown = await readFile((await download.path())!, 'utf8')
  expect(markdown.startsWith(`# ${title}\n\n`)).toBe(true)
  expect(markdown).toContain(firstMessage)
  expect(markdown).toContain('General Project write completed: alpha-project-only.')
  expect(markdown).not.toContain('e2e-project-proof.txt')
  expect(markdown.match(/^## 用户$/gm)).toHaveLength(turns)
  expect(markdown.match(/^## Agent$/gm)).toHaveLength(turns)
  expect(markdown.indexOf('Saved turn 1\n')).toBeLessThan(markdown.indexOf(`Saved turn ${turns - 1}\n`))
  await expect(dialog).toBeVisible()
  await dialog.getByRole('button', { name: `打开 ${title}`, exact: true }).click()
  await expect(dialog).toBeHidden()
  await expect(page.getByPlaceholder(/输入消息/).filter({ visible: true })).toBeVisible()

  const settings = await (await request.get('/api/settings')).json()
  const changed = await request.patch('/api/settings', { data: {
    layer: 'user', base_revision: settings.revisions.user, changes: { theme: 'light' },
  } })
  expect(changed.ok(), await changed.text()).toBe(true)
  await page.setViewportSize({ width: 390, height: 844 })
  await page.reload()
  await page.getByRole('button', { name: '项目目录', exact: true }).click()
  await page.getByRole('button', { name: '对话历史', exact: true }).click()
  await search.fill('灯塔档案')
  await expect(dialog.locator('mark')).toHaveText('灯塔档案')
  await expect(dialog.locator('mark')).toBeInViewport({ ratio: 1 })
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  await page.screenshot({ path: test.info().outputPath('history-light-narrow.png'), animations: 'disabled' })
  await search.fill('missing-archive-content')
  await expect(dialog.getByText('没有匹配的历史对话。')).toBeVisible()
  await page.screenshot({ path: test.info().outputPath('history-empty.png'), animations: 'disabled' })

  const pending = request.post(`${apiPath}/chat`, { data: {
    command_id: `archive-${session.id}-running`, session_id: session.id,
    message: 'Pending export snapshot. E2E_DELAYED_AGENT_REPLY',
  } })
  try {
    await expect.poll(async () => (await getModelStatus(request)).delayed_waiting_by_marker.E2E_DELAYED_AGENT_REPLY ?? 0).toBe(1)
    const english = await request.get(`${apiPath}/session/export?session_id=${session.id}`, {
      headers: { 'X-Denova-Locale': 'en-US' },
    })
    expect(english.ok()).toBe(true)
    const snapshot = await english.text()
    expect(snapshot).toContain('## User\n\n')
    expect(snapshot).toContain('Pending export snapshot.')
    expect(snapshot).not.toContain('Recovered response completed exactly once.')
  } finally {
    await releaseDelayedRequest(request, 'E2E_DELAYED_AGENT_REPLY')
    expect((await pending).ok()).toBe(true)
  }
})
