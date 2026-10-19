import { expect, test } from '../support/fixtures'
import { createAndOpenBook, createProjectFile } from '../support/api'

test('loads the source editor and its worker without public network access', { tag: '@smoke' }, async ({ page, request }) => {
  const book = await createAndOpenBook(request, 'Offline Source Editor')
  await createProjectFile(request, book.projectId, 'chapters/offline.md', '# Offline\n\nLocal editor content.\n')
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/')
  await page.getByRole('button', { name: '文件', exact: true }).click()
  await page.getByRole('dialog', { name: '项目', exact: true }).getByRole('button', { name: /^offline/ }).click()
  await page.getByRole('button', { name: '编辑工具', exact: true }).click()
  // Rich-text writing must not preload the multi-megabyte source runtime.
  expect(await page.evaluate(() => performance.getEntriesByType('resource')
    .map(entry => new URL(entry.name).pathname).filter(path => /^\/assets\/monaco[-.]/.test(path)))).toEqual([])
  const workerStarted = page.waitForEvent('worker')
  await page.getByRole('menuitemcheckbox', { name: '源码', exact: true }).click()
  await expect(page.locator('.monaco-editor:visible')).toBeVisible()
  await expect(page.locator('.monaco-editor .view-lines')).toContainText('Local editor content.')
  const worker = await workerStarted
  expect(new URL(worker.url()).origin).toBe(new URL(page.url()).origin)
})

test('loads the preset JSON editor on demand and returns to the visual editor', async ({ page, request }) => {
  await createAndOpenBook(request, 'Offline Preset Editor')
  const response = await request.post('/api/rule-systems', { data: {
    name: 'Offline JSON Rules', trpg_system: { rule_templates: [] },
  } })
  expect(response.ok(), await response.text()).toBe(true)
  await page.goto('/')
  await page.getByLabel('工作台侧边栏').getByRole('button', { name: '创作方案', exact: true }).click()
  await page.locator('.preset-directory input').fill('Offline JSON Rules')
  await page.locator('.preset-directory').getByRole('button', { name: /Offline JSON Rules/ }).click()
  await expect(page.getByTestId('preset-config-visual-editor')).toBeVisible()
  expect(await page.evaluate(() => performance.getEntriesByType('resource')
    .map(entry => new URL(entry.name).pathname).filter(path => /^\/assets\/monaco[-.]/.test(path)))).toEqual([])
  await page.getByRole('button', { name: 'JSON', exact: true }).click()
  await expect(page.getByTestId('story-director-json-editor').locator('.monaco-editor .view-lines')).toContainText('rule_templates')
  await page.getByRole('button', { name: '可视化', exact: true }).click()
  await expect(page.getByTestId('preset-config-visual-editor')).toBeVisible()
})
