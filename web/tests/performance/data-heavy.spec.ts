import { expect, test } from '../support/fixtures'
import { createAndOpenBook, readProjectFile, saveProjectFile, createAgentChatSession } from '../support/api'
import { installAudit, measure, probe, saveAudit, visit } from './page-audit'

const main = '[data-testid="main"] section[aria-hidden="false"]'

for (const size of [100, 1000]) {
  test(`profiles library and version reads with ${size} records`, async ({ page, request }) => {
    test.setTimeout(300000)
    const book = await createAndOpenBook(request, `Data audit ${size}`)
    const project = `/api/projects/${book.projectId}`
    const body = '车站的灯光照亮漫长的轨道。The story continues.\n'.repeat(100)
    const items = Array.from({ length: size }, (_, index) => ({
      id: `perf-${index}`, name: `Audit character ${index}`, type: 'character', enabled: true,
      brief_description: 'A traveler waiting at the old station.', content: body,
    }))
    const lore = JSON.parse((await readProjectFile(request, book.projectId, 'setting/lore/items.json')).content)
    await saveProjectFile(request, book.projectId, 'setting/lore/items.json', JSON.stringify({ ...lore, items }))
    for (let start = 0; start < size; start += 100) {
      const response = await request.post(`${project}/files/operations`, { data: {
        operations: Array.from({ length: Math.min(100, size - start) }, (_, index) => ({
          id: `${start + index}`, kind: 'create', type: 'file', path: `chapters/ch${start + index + 1}-audit.md`,
          content: `${body}\nUnique chapter ${start + index + 1}.`,
        })),
      } })
      expect(response.ok(), await response.text()).toBe(true)
      expect((await response.json()).results.every((item: { ok: boolean }) => item.ok)).toBe(true)
    }
    const version = await request.post(`${project}/versions`, { data: { message: 'Audit dataset' } })
    expect(version.ok(), await version.text()).toBe(true)
    await installAudit(page)
    await page.addInitScript(() => localStorage.setItem('nova:mode', 'ide'))
    const library = await visit(page, '资料库', { selector: '[data-testid="lore-card-perf-0"]' })
    saveAudit(`data-${size}-partial`, { size, bodyChars: body.length, library })
    if (process.env.DENOVA_PERFORMANCE_PROFILE === '1') {
      await page.getByLabel('工作台侧边栏').getByRole('button', { name: '写作', exact: true }).click()
      await expect(page.getByLabel('作品大纲')).toBeVisible()
      const profiler = await page.context().newCDPSession(page)
      await profiler.send('Profiler.enable')
      await profiler.send('Profiler.start')
      const timing = await measure(page, { selector: '[data-testid="lore-card-perf-0"]' }, () => page.getByLabel('工作台侧边栏').getByRole('button', { name: '资料库', exact: true }).click())
      const { profile } = await profiler.send('Profiler.stop')
      saveAudit(`profile-${size}`, { timing, profile })
      await profiler.detach()
    }
    const search = []
    for (let attempt = 0; attempt < 3; attempt++) {
      const input = page.getByPlaceholder('搜索名称、简介、标签或正文…')
      search.push(await measure(page, { selector: '[data-testid="lore-library"]', text: '没有匹配的资料' }, () => input.fill('no-matching-audit-record')))
      await input.fill('')
      await expect(page.getByTestId('lore-card-perf-0')).toBeVisible()
    }
    await page.screenshot({ path: `test-results/page-audit-library-${size}.png` })
    const versions = await visit(page, '版本管理', { selector: `${main} button[aria-label="恢复此文件"]` })
    const api: Record<string, unknown> = {}
    for (const path of ['book/tree', 'book/summary', 'book/lore/items', 'book/lore/categories', 'versions/status', 'versions?limit=30']) {
      api[path] = await probe(request, `${project}/${path}`)
    }
    const versionRows = (await (await request.get(`${project}/versions?limit=30`)).json()).versions
    api['version/diff'] = await probe(request, `${project}/versions/${versionRows[0].id}/diff?comparison=parent`)
    saveAudit(`data-${size}`, { size, bodyChars: body.length, library, search, versions, api })

    if (size === 1000) {
      // Performance must retain offscreen editing, complete batch selection,
      // keyboard sorting and invalidation from canonical file updates.
      await page.getByLabel('工作台侧边栏').getByRole('button', { name: '资料库', exact: true }).click()
      const overview = page.getByTestId('lore-library')
      await expect(overview).toBeVisible()
      await overview.getByRole('button', { name: '批量操作', exact: true }).click()
      await overview.getByRole('button', { name: '全选当前结果', exact: true }).click()
      await expect(overview.getByRole('toolbar').getByRole('status')).toHaveText('已选 1000 项')
      await overview.getByRole('button', { name: '退出多选', exact: true }).click()
      const first = overview.getByRole('button', { name: '排序资料：Audit character 0', exact: true })
      await first.press('Space')
      await first.press('ArrowRight')
      await first.press('Space')
      await expect.poll(() => overview.locator('[data-slot="card-title"]').first().textContent()).toBe('Audit character 1')
      await overview.evaluate(element => { element.scrollTop = element.scrollHeight })
      await expect(overview.getByTestId('lore-card-perf-999')).toBeVisible()
      const enabled = overview.getByTestId('lore-card-perf-999').getByRole('switch')
      await enabled.click()
      await expect(enabled).not.toBeChecked()
      const document = JSON.parse((await readProjectFile(request, book.projectId, 'setting/lore/items.json')).content)
      const last = document.items.find((item: { id: string }) => item.id === 'perf-999')
      expect(last.enabled).toBe(false)
      last.content += '\nExternal cached marker'
      await saveProjectFile(request, book.projectId, 'setting/lore/items.json', JSON.stringify(document))
      await overview.getByPlaceholder('搜索名称、简介、标签或正文…').fill('External cached marker')
      await expect(overview.getByTestId('lore-card-perf-999')).toBeVisible()
      await expect(overview.getByTestId('lore-card-perf-999').getByRole('switch')).not.toBeChecked()
    }
  })
}

test('profiles books and automation catalogs at two scales', async ({ page, request }) => {
  test.setTimeout(300000)
  await installAudit(page)
  await page.addInitScript(() => localStorage.setItem('nova:mode', 'ide'))
  const projects: string[] = []
  const result = []
  for (const size of [20, 100]) {
    while (projects.length < size) {
      const book = await createAndOpenBook(request, `Catalog audit ${projects.length}`)
      projects.push(book.projectId)
      const response = await request.post('/api/automations', { data: {
        scope: 'workspace', target: { kind: 'workspace', project_id: book.projectId },
        enabled: false, name: `Audit task ${projects.length}`, template: 'custom_prompt', prompt: 'Review the current work.',
      } })
      expect(response.ok(), await response.text()).toBe(true)
    }
    const books = await visit(page, '书籍管理', { selector: `${main} button`, text: 'Catalog audit' })
    const automations = await visit(page, '自动化', { selector: 'button', text: `Audit task ${size}` })
    await page.screenshot({ path: `test-results/page-audit-automation-${size}.png` })
    result.push({ size, actualBookCount: (await (await request.get('/api/books')).json()).books.length,
      books, automations, api: {
        books: await probe(request, '/api/books'),
        catalog: await probe(request, '/api/automations/catalog'),
        projects: await probe(request, '/api/agent-chat/projects'),
      } })
    saveAudit('catalog', result)
  }
})

test('profiles conversation history and full-text search', async ({ page, request }) => {
  test.setTimeout(1200000)
  await installAudit(page)
  await page.addInitScript(() => localStorage.setItem('nova:mode', 'ide'))
  const book = await createAndOpenBook(request, 'History audit')
  const apiPath = `/api/projects/${book.projectId}/agent-chat`
  const archiveBody = '车站的灯光照亮漫长的轨道。'.repeat(500)
  const result = []
  let created = 0
  for (const size of [50, 200]) {
    while (created < size) {
      const session = await createAgentChatSession(request, book.projectId, `Audit conversation ${created}`)
      const response = await request.post(`${apiPath}/chat`, { data: {
        command_id: `audit-${session.id}`, session_id: session.id,
        message: `${archiveBody} End of archive ${created}`,
      } })
      expect(response.ok(), await response.text()).toBe(true)
      created++
    }
    const route = await visit(page, '工作台', { selector: 'button[aria-label="对话历史"]' })
    const history = []
    const search = []
    for (let attempt = 0; attempt < 3; attempt++) {
      history.push(await measure(page, { selector: '[role="dialog"] button', text: 'Audit conversation' }, () => page.getByRole('button', { name: '对话历史', exact: true }).click()))
      const dialog = page.getByRole('dialog', { name: '对话历史', exact: true })
      search.push(await measure(page, { selector: '[role="dialog"]', text: '没有匹配的历史对话。' }, () => dialog.getByRole('textbox', { name: '搜索对话历史' }).fill('nonexistent-audit-needle')))
      await dialog.getByRole('textbox', { name: '搜索对话历史' }).fill('')
      await expect(dialog.getByRole('button', { name: /^打开 Audit conversation/ }).first()).toBeVisible()
      await dialog.press('Escape')
      await expect(dialog).toBeHidden()
    }
    result.push({ size, userCharsPerSession: archiveBody.length, route, history, search, api: {
      list: await probe(request, `/api/agent-chat/history?project_id=${book.projectId}&limit=80`),
      search: await probe(request, `/api/agent-chat/history?project_id=${book.projectId}&limit=80&query=nonexistent-audit-needle`),
    } })
    saveAudit('history', result)
  }
})
