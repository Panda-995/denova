import { expect, test } from '../support/fixtures'
import { createAndOpenBook, createProjectFile, readProjectFile, saveProjectFile } from '../support/api'

test('retains the open Lore tab and refreshes it without blocking chapter navigation', async ({ page, request }) => {
  const book = await createAndOpenBook(request, 'Writing tab cache')
  const chapterPath = 'chapters/cache-chapter.md'
  await createProjectFile(request, book.projectId, chapterPath, '# Cached chapter\n\nOriginal chapter.')
  const lorePath = `/api/projects/${book.projectId}/book/lore/items`
  const created = await request.post(lorePath, { data: {
    id: 'cache-hero', name: '缓存人物', type: 'character', content: 'Original lore.',
  } })
  expect(created.ok()).toBe(true)
  await page.goto('/')
  await page.getByLabel('工作台侧边栏').getByRole('button', { name: '写作', exact: true }).click()
  await page.getByRole('button', { name: /^cache chapter\b/ }).click()
  const chapter = page.getByTestId('main').getByRole('textbox', { name: '', exact: true })
  await expect(chapter).toContainText('Original chapter.')
  await page.getByTestId('book-settings-header-frame').getByRole('button', { name: '设定', exact: true }).click()
  const workspace = page.getByRole('region', { name: '作品设定', exact: true })
  const body = workspace.getByRole('textbox', { name: '编辑设定：缓存人物', exact: true })
  await expect(body).toContainText('Original lore.')
  const retained = await body.elementHandle()
  await body.fill('Saved while switching tabs.')
  await page.getByRole('tab', { name: /cache chapter/ }).click()
  // Hidden editors must release the shared save handler to the current chapter.
  await expect(workspace).toBeHidden()
  expect(await retained!.evaluate(element => element.isConnected)).toBe(true)
  await chapter.fill('Saved chapter with a retained Lore tab.')

  const items = (await (await request.get(lorePath)).json()).items
  expect(items.find((item: { id: string }) => item.id === 'cache-hero').content).toContain('Saved while switching tabs.')
  let release!: () => void
  const gate = new Promise<void>(resolve => { release = resolve })
  let reads = 0
  await page.route(`**${lorePath}`, async route => {
    if (route.request().method() === 'GET') {
      reads++
      await gate
    }
    await route.continue()
  })
  try {
    const hero = items.find((item: { id: string }) => item.id === 'cache-hero')
    const external = await request.put(`${lorePath}/cache-hero`, { data: {
      ...hero, content: 'Changed outside the cached tab.', base_revision: hero.updated_at,
    } })
    expect(external.ok(), await external.text()).toBe(true)
    await page.getByRole('tab', { name: '设定', exact: true }).click()
    await expect(workspace).toBeVisible()
    expect(await retained!.evaluate(element => element.isConnected)).toBe(true)
    await expect(body).toContainText('Saved while switching tabs.')
    await expect(page.locator('[data-slot=loading-state]:visible')).toHaveCount(0)
    await expect.poll(() => reads).toBeGreaterThan(0)
    expect((await readProjectFile(request, book.projectId, chapterPath)).content).toContain('Saved chapter with a retained Lore tab.')
  } finally { release() }
  await expect(body).toContainText('Changed outside the cached tab.')
})

test('retains chapter editors and undo history while revalidating cached documents', async ({ page, request }) => {
  const book = await createAndOpenBook(request, 'Retained chapter editors')
  const path = 'chapters/alpha.md'
  await createProjectFile(request, book.projectId, path, '# Alpha\n\nOriginal paragraph.\n')
  await createProjectFile(request, book.projectId, 'chapters/beta.md', '# Beta\n\nSecond chapter.\n')
  await page.goto('/')
  await page.getByRole('button', { name: /^alpha\b/ }).click()
  const editor = page.getByTestId('main').getByRole('textbox', { name: '', exact: true })
  await expect(editor).toContainText('Original paragraph.')
  const retained = await editor.elementHandle()
  await editor.press('ControlOrMeta+End')
  await editor.press('Enter')
  await editor.pressSequentially('A retained edit.')
  await page.getByRole('button', { name: /^beta\b/ }).click()
  await expect(editor).toContainText('Second chapter.')
  expect(await retained!.evaluate(element => element.isConnected)).toBe(true)
  expect((await readProjectFile(request, book.projectId, path)).content).toContain('A retained edit.')

  await page.getByTestId('book-settings-header-frame').getByRole('button', { name: '设定', exact: true }).click()
  await expect(page.getByRole('region', { name: '作品设定', exact: true })).toBeVisible()
  expect(await retained!.evaluate(element => element.isConnected)).toBe(true)
  let release!: () => void
  const gate = new Promise<void>(resolve => { release = resolve })
  let reads = 0
  await page.route(`**/api/projects/${book.projectId}/files/file?*`, async route => {
    if (route.request().method() === 'GET' && new URL(route.request().url()).searchParams.get('path') === path) {
      reads++
      await gate
    }
    await route.continue()
  })
  try {
    await page.getByRole('tab', { name: /alpha/ }).click()
    await expect(editor).toContainText('A retained edit.', { timeout: 3000 })
    await expect.poll(() => reads).toBeGreaterThan(0)
    await editor.press('ControlOrMeta+z')
    await expect(editor).not.toContainText('A retained edit.')
  } finally { release() }
  await page.getByRole('tab', { name: /beta/ }).click()
  await expect(editor).toContainText('Second chapter.')
  await saveProjectFile(request, book.projectId, path, '# Alpha\n\nChanged outside the editor.\n')
  await page.getByRole('tab', { name: /alpha/ }).click()
  await expect(editor).toContainText('Changed outside the editor.')
  await editor.press('ControlOrMeta+End')
  await editor.press('Enter')
  await editor.pressSequentially('Saved after external change.')
  await page.getByRole('tab', { name: /beta/ }).click()
  expect((await readProjectFile(request, book.projectId, path)).content).toContain('Saved after external change.')
})
