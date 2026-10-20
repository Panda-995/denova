import { randomUUID } from 'node:crypto'
import { readFile } from 'node:fs/promises'
import path from 'node:path'
import { expect, test, type APIRequestContext } from '../support/fixtures'
import { createAndOpenBook, createProjectFile, createStartedStory, getStoryBranches } from '../support/api'
import type { NarrativeMentionPage } from '../../src/features/lore/mentions-api'

const name = '林川😀'

async function snapshot(request: APIRequestContext, story: string, branch?: string) {
  const response = await request.get(`/api/interactive/stories/${story}/snapshot${branch ? `?branch=${branch}` : ''}`)
  expect(response.ok(), await response.text()).toBe(true)
  return response.json()
}

async function revise(request: APIRequestContext, story: string, branch: string, turn: { id: string; narrative: string }, narrative: string) {
  const response = await request.patch(`/api/interactive/stories/${story}/turns/${turn.id}/narrative`, {
    data: { branch_id: branch, expected_narrative: turn.narrative, narrative },
  })
  expect(response.ok(), await response.text()).toBe(true)
}

async function mentions(request: APIRequestContext, project: string, story: string, branch: string, query: string, before = '') {
  const response = await request.get(`/api/projects/${project}/interactive/stories/${story}/mentions?${new URLSearchParams({ branch, q: query, before })}`)
  expect(response.ok(), await response.text()).toBe(true)
  return (await response.json()).page as NarrativeMentionPage
}

test('finds saved text and exact branch narrative without moving Game progress', async ({ page, request, browserDiagnostics }, testInfo) => {
  test.setTimeout(240_000)
  const other = await createAndOpenBook(request, 'Unrelated foreground')
  const book = await createAndOpenBook(request, 'Lore mention evidence')
  const text = `# 长章节\n\n${'远处灯火。'.repeat(300)}${name}走过车站。又遇见${name}。\n`
  await createProjectFile(request, book.projectId, 'chapters/mentions.md', text)
  await createProjectFile(request, book.projectId, `chapters/${name}.md`, '这里只有文件名匹配。')
  const lore = await request.post(`/api/projects/${book.projectId}/book/lore/items`, {
    data: { id: 'hero', name, type: 'character', enabled: true, content: '角色设定正文。' },
  })
  expect(lore.ok(), await lore.text()).toBe(true)
  const story = await createStartedStory(request, 'E2E_LORE_MENTIONS_OPENING')
  const first = (await snapshot(request, story.id)).current_turn
  const longNarrative = `${'远处灯火，长夜未尽。'.repeat(200)}\n${name}在共同历史中出现。\n${name}再次出现。`
  expect(first.narrative).toBe(longNarrative)
  const a = await request.post(`/api/interactive/stories/${story.id}/branches`, { data: { parent_event_id: first.id, title: '支线独有' } })
  expect(a.ok(), await a.text()).toBe(true)
  const branchA = (await a.json()).id
  const submit = async (branch: string) => {
    const before = (await snapshot(request, story.id, branch)).current_turn.id
    const response = await request.post('/api/interactive/chat', {
      data: { command_id: `mentions-${randomUUID()}`, mode: 'story', story_id: story.id, branch, message: '只有输入异词，推开石门。' },
    })
    expect(response.ok(), await response.text()).toBe(true)
    await expect.poll(async () => (await snapshot(request, story.id, branch)).current_turn.id).not.toBe(before)
  }
  await submit(branchA)
  const aTurn = (await snapshot(request, story.id, branchA)).current_turn
  const aNarrative = `${name}在支线独有的桥头。`
  await revise(request, story.id, branchA, aTurn, aNarrative)
  const b = await request.post(`/api/interactive/stories/${story.id}/branches`, { data: { parent_event_id: first.id, title: '当前进度' } })
  expect(b.ok(), await b.text()).toBe(true)
  const branchB = (await b.json()).id
  // The matching ancestor is outside both the initial display window and one search batch.
  for (let turn = 0; turn < 22; turn++) await submit(branchB)
  // Side revisions stay in their owning branch, including when the base Turn is shared.
  await revise(request, story.id, 'main', first, '主线限定：重写了开场。')
  const before = await snapshot(request, story.id)
  expect(before.branch_id).toBe(branchB)
  expect(before.turns.some((turn: { id: string }) => turn.id === first.id)).toBe(false)
  const journalPath = path.join(book.workspace, 'interactive/story', `story-${story.id}.jsonl`)
  const journal = await readFile(journalPath)
  const lorePath = path.join(book.workspace, 'setting/lore/items.json')
  const loreBytes = await readFile(lorePath)

  const savedSearch = await request.get(`/api/projects/${book.projectId}/workspace/search?${new URLSearchParams({ q: name, text_mentions: '1' })}`)
  expect(savedSearch.ok()).toBe(true)
  const fileHits = (await savedSearch.json()).results
  expect(fileHits.filter((hit: { path: string }) => hit.path === 'chapters/mentions.md')).toEqual([
    expect.objectContaining({ line: 3, column: 1501, match_text: name, preview: expect.stringContaining(name) }),
    expect.objectContaining({ line: 3, match_text: name, preview: expect.stringContaining(name) }),
  ])
  expect(fileHits.some((hit: { path: string }) => hit.path === `chapters/${name}.md`)).toBe(false)
  expect(fileHits).toHaveLength(2)

  await page.goto('/')
  const sidebar = page.getByLabel('工作台侧边栏')
  await sidebar.getByRole('button', { name: '游戏', exact: true }).click()
  await expect(page.getByPlaceholder(/你要做什么/)).toBeVisible()
  await sidebar.getByRole('button', { name: '资料库', exact: true }).click()
  await page.getByTestId('lore-card-hero').getByRole('button', { name, exact: true }).click()
  await page.getByRole('tab', { name: '作品提及', exact: true }).click()
  const panel = page.getByTestId('lore-mentions').filter({ visible: true })
  await expect(panel.getByLabel('查找文字', { exact: true })).toHaveValue(name)
  await expect(panel.getByText('查找这条设定在作品中的提及', { exact: true })).toBeVisible()
  await panel.getByRole('button', { name: '查找', exact: true }).click()
  const fileGroup = panel.getByTestId('lore-mention-group').filter({ hasText: 'mentions.md' })
  await fileGroup.getByRole('button', { expanded: false }).click()
  await expect(fileGroup.getByText('第 3 行').first()).toBeVisible()
  await fileGroup.getByRole('button', { name: '打开文件', exact: true }).first().click()
  await expect(page.locator('.ProseMirror:visible').filter({ hasText: '长章节' })).toContainText(`${name}走过车站。`)
  await expect(page.locator('.ProseMirror:visible .nova-search-current').first()).toBeInViewport()
  await sidebar.getByRole('button', { name: '资料库', exact: true }).click()
  await expect(fileGroup.getByRole('button', { expanded: true })).toBeVisible()
  await expect(panel.getByLabel('查找文字', { exact: true })).toHaveValue(name)

  await panel.getByRole('radio', { name: '故事正文', exact: true }).click()
  await expect(panel.getByText('本批回合没有提及，可以继续查找更早的回合。', { exact: true })).toBeVisible()
  await panel.getByRole('button', { name: '继续查找更早回合', exact: true }).click()
  await expect(panel.getByText('已查找 23 个回合 · 1 个回合有提及', { exact: true })).toBeVisible()
  const turnGroup = panel.getByTestId('lore-mention-group')
  await expect(turnGroup).toHaveCount(1)
  await turnGroup.getByRole('button', { expanded: false }).click()
  await expect(turnGroup).toContainText(`${name}在共同历史中出现。`)
  await turnGroup.getByRole('button', { name: '查看回合原文', exact: true }).click()
  const source = panel.getByTestId('lore-mention-source')
  await expect(source).toHaveText(longNarrative)
  await expect(source.locator('mark').first()).toBeInViewport()
  await expect(panel.getByRole('button', { name: '返回提及列表', exact: true })).toBeInViewport()
  await page.screenshot({ path: testInfo.outputPath('mentions-historical-source.png') })
  await page.keyboard.press('Escape')
  await expect(turnGroup.getByRole('button', { name: '查看回合原文', exact: true })).toBeFocused()
  await expect(turnGroup.getByRole('button', { expanded: true })).toBeVisible()
  await page.screenshot({ path: testInfo.outputPath('mentions-dark-wide.png') })

  await panel.getByRole('combobox', { name: '查找分支', exact: true }).click()
  await page.getByRole('option', { name: '支线独有', exact: true }).click()
  await expect(panel.getByTestId('lore-mention-group')).toHaveCount(2)
  expect((await snapshot(request, story.id)).current_turn.id).toBe(before.current_turn.id)
  expect((await getStoryBranches(request, story.id)).find(branch => branch.current)?.id).toBe(branchB)
  expect(await readFile(journalPath)).toEqual(journal)
  expect(await readFile(lorePath)).toEqual(loreBytes)
  expect(await readFile(path.join(book.workspace, 'chapters/mentions.md'), 'utf8')).toBe(text)
  // Full user/state/Director-only strings are never narrative matches.
  for (const query of ['只有输入异词', '石门已经开启，前方出现一座旧车站', '保留玩家离开车站的自由']) {
    expect((await mentions(request, book.projectId, story.id, branchA, query)).hits).toEqual([])
  }
  const oldHit = (await mentions(request, book.projectId, story.id, branchA, name)).hits.find(hit => hit.turn_id === aTurn.id)!
  await revise(request, story.id, branchA, { ...aTurn, narrative: aNarrative }, '桥头已经无人。')
  browserDiagnostics.allow(/console\.error: Failed to load resource:.*400.*\/api\/projects\/.*\/narrative/)
  const latestGroup = panel.getByTestId('lore-mention-group').first()
  await latestGroup.getByRole('button', { expanded: false }).click()
  await latestGroup.getByRole('button', { name: '查看回合原文', exact: true }).click()
  await expect(panel.getByText(/原文不可用或已经修改/)).toBeVisible()
  await expect(source).toHaveCount(0)
  const afterEdit = await readFile(journalPath)
  const stale = await request.get(`/api/projects/${book.projectId}/interactive/stories/${story.id}/narrative?${new URLSearchParams({ branch: branchA, turn_id: oldHit.turn_id, revision: oldHit.source_revision })}`)
  expect(stale.status()).toBe(400)
  expect(await readFile(journalPath)).toEqual(afterEdit)

  // A different foreground Project must not retarget an existing Lore read.
  const switched = await page.request.post('/api/workspace/switch', { data: { path: other.workspace } })
  expect(switched.ok(), await switched.text()).toBe(true)
  const scoped = await mentions(request, book.projectId, story.id, branchA, name)
  expect(scoped.hits.map(hit => hit.turn_id)).toEqual([first.id])
  const foreign = await request.get(`/api/projects/${book.projectId}/interactive/stories/${story.id}/narrative?${new URLSearchParams({ branch: branchB, turn_id: aTurn.id, revision: oldHit.source_revision })}`)
  expect(foreign.ok()).toBe(false)
  expect(await readFile(journalPath)).toEqual(afterEdit)
})

test('keeps the shared mention flow readable in English, light theme and narrow layouts', async ({ page, request }, testInfo) => {
  const book = await createAndOpenBook(request, 'Mention layouts')
  await createProjectFile(request, book.projectId, `chapters/${'LongSourceName'.repeat(10)}.md`, Array.from({ length: 105 }, (_, index) => `${index}: Port😀 appears beside ${'a distant station '.repeat(8)}.`).join('\n'))
  const lore = await request.post(`/api/projects/${book.projectId}/book/lore/items`, {
    data: { id: 'hero', name: 'A very long character name for a narrow authoring workspace', type: 'character', enabled: true, content: 'An ordinary character.' },
  })
  expect(lore.ok()).toBe(true)
  const settings = await request.get('/api/settings')
  const changed = await request.patch('/api/settings', { data: { layer: 'user', base_revision: (await settings.json()).revisions.user, changes: { theme: 'light', language: 'en-US' } } })
  expect(changed.ok()).toBe(true)
  await page.goto('/')
  await page.getByLabel('Workbench sidebar').getByRole('button', { name: 'Lore', exact: true }).click()
  await page.getByTestId('lore-card-hero').getByRole('button', { name: /^A very long character name/ }).click()
  await page.getByRole('tab', { name: 'Mentions in work', exact: true }).click()
  const panel = page.getByTestId('lore-mentions').filter({ visible: true })
  await panel.getByLabel('Find text', { exact: true }).fill('DoesNotExist😀')
  await panel.getByLabel('Find text', { exact: true }).press('Enter')
  await expect(panel.getByText('No matching text found', { exact: true })).toBeVisible()
  await panel.getByRole('radio', { name: 'Story narrative', exact: true }).click()
  await expect(panel.getByText('No matching text found', { exact: true })).toBeVisible()
  await panel.getByRole('radio', { name: 'Project files', exact: true }).click()
  await panel.getByLabel('Find text', { exact: true }).fill('Port😀')
  await panel.getByRole('button', { name: 'Find', exact: true }).click()
  await expect(panel.getByText('Showing the first 100 mentions. Narrow your search for more precise results.', { exact: true })).toBeVisible()
  for (const width of [390, 320, 1440]) {
    await page.setViewportSize({ width, height: 900 })
    await expect(panel.getByLabel('Find text', { exact: true })).toBeVisible()
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
    expect(await panel.evaluate(element => element.scrollWidth <= element.clientWidth)).toBe(true)
    await page.screenshot({ path: testInfo.outputPath(`mentions-light-en-${width}.png`) })
  }
  const chinese = await request.patch('/api/settings', { data: { layer: 'user', changes: { language: 'zh-CN' } } })
  expect(chinese.ok()).toBe(true)
  await page.reload()
  await page.getByLabel('工作台侧边栏').getByRole('button', { name: '写作', exact: true }).click()
  await page.getByTestId('book-settings-header-frame').getByRole('button', { name: '设定', exact: true }).click()
  const writing = page.getByRole('region', { name: '作品设定', exact: true })
  await expect(writing.getByRole('button', { name: /^修改名称：A very long character name/ })).toBeVisible()
  await writing.getByRole('tab', { name: '作品提及', exact: true }).click()
  await writing.getByLabel('查找文字', { exact: true }).fill('Port😀')
  await writing.getByRole('button', { name: '查找', exact: true }).click()
  await expect(writing.getByText('仅显示前 100 处提及，请缩小查找范围。', { exact: true })).toBeVisible()
  await page.screenshot({ path: testInfo.outputPath('mentions-writing-inline.png') })
})
