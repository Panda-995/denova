import { cp, mkdtemp } from 'node:fs/promises'
import path from 'node:path'
import { runtimeRoot } from '../../scripts/e2e-paths.mjs'
import { expect, test, type APIRequestContext, type Locator, type Page } from '../support/fixtures'
import { createAndOpenBook, enablePlugin, registerAgentChatProject } from '../support/api'
import { modelControlURL, type E2EModelRequest } from '../support/model'

const source = process.env.DENOVA_TEST_COMIC_PLUGIN

test('native plugin Lore preserves the comic and Agent, flushes edits, and references the current Project', async ({ page, request, browserDiagnostics }, testInfo) => {
  test.skip(!source, 'Set DENOVA_TEST_COMIC_PLUGIN to the index comic plugin directory')
  browserDiagnostics.allow(/console.error: Failed to load resource:.*404.*project-content\/file\?path=comic%2Fcomic.json/)
  // Episode settings are optional until a script or reference plan is saved.
  browserDiagnostics.allow(/console.error: Failed to load resource:.*404.*project-content\/file\?path=comic%2Fepisodes%2F[0-9a-f-]+%2Fepisode\.json/)
  const alpha = await createAndOpenBook(request, 'Native comic Lore Alpha')
  const beta = await createAndOpenBook(request, 'Native comic Lore Beta')
  await installComic(request, alpha.projectId, alpha.workspace)
  await configureComic(request, beta.projectId)
  await createLore(request, alpha.projectId, '甲项目角色', 'COMIC_ALPHA_ORIGINAL_BODY')
  await createLore(request, beta.projectId, '乙项目角色', 'COMIC_BETA_PRIVATE_BODY')
  expect((await request.post('/api/workspace/switch', { data: { path: alpha.workspace } })).ok()).toBe(true)
  await page.setViewportSize({ width: 1600, height: 1000 })
  await page.addInitScript(() => localStorage.setItem('nova:mode', 'plugin:index.comic-studio/studio'))
  await page.goto('/')
  const studio = page.frameLocator('iframe[title="插件界面"]')
  await studio.getByRole('button', { name: '新分话', exact: true }).first().click()
  for (const number of [1, 2]) {
    await studio.getByRole('button', { name: '新页面', exact: true }).first().click()
    await expect(studio.getByRole('heading', { name: `第 1 话 / 第 ${number} 页`, exact: true })).toBeVisible()
  }
  const frame = await page.locator('iframe[title="插件界面"]').elementHandle()
  const native = page.locator('[data-plugin-host-tab="lore"]')
  const agent = page.locator('#plugin-page-agent')
  const composer = agent.getByRole('textbox')
  await composer.fill('保留尚未发送的漫画构思')
  await studio.getByRole('button', { name: '设定', exact: true }).click()
  await expect(native).toBeVisible()
  await expect(page.locator('iframe[title="插件界面"]')).toBeHidden()
  await expect(composer).toHaveText('保留尚未发送的漫画构思')
  await expect(agent.getByRole('button', { name: '生成整页', exact: true })).toHaveCount(0)
  await native.getByRole('button', { name: /^甲项目角色/ }).click()
  const editor = native.getByRole('textbox', { name: '编辑设定：甲项目角色', exact: true })
  await editor.fill('Comic lore freshly edited body')
  await native.getByRole('button', { name: '在创作 Agent 中引用此设定', exact: true }).click()
  await expect(composer).toContainText('保留尚未发送的漫画构思')
  await page.screenshot({ path: testInfo.outputPath('comic-native-lore-dark.png'), fullPage: true })
  await composer.pressSequentially(' E2E_RUNTIME_COMIC_LORE_TAB')
  const submitted = page.waitForRequest(req => req.method() === 'POST' && req.url().endsWith(`/api/projects/${alpha.projectId}/agent-chat/chat`))
  await agent.locator('[data-action="send"]').click()
  const payload = (await submitted).postDataJSON()
  expect(payload.lore_references).toEqual(['shared-character-id'])
  expect(payload.page_context).toEqual({ plugin_id: 'index.comic-studio', page_id: 'studio', data: { surface: 'lore', itemId: 'shared-character-id' } })
  await expect.poll(async () => (await (await request.get(`${modelControlURL}/control/runtime-requests?marker=E2E_RUNTIME_COMIC_LORE_TAB`)).json()).length).toBeGreaterThan(0)
  const captured = await (await request.get(`${modelControlURL}/control/runtime-requests?marker=E2E_RUNTIME_COMIC_LORE_TAB`)).json() as E2EModelRequest[]
  expect(JSON.stringify(captured[0].messages)).toContain('Comic lore freshly edited body')
  expect(JSON.stringify(captured[0].messages)).not.toContain('COMIC_BETA_PRIVATE_BODY')
  await expect(agent.getByText('Deterministic E2E response completed.', { exact: true })).toBeVisible()
  // A failed save must keep the native editor and its draft owned and visible.
  const saveURL = `**/api/projects/${alpha.projectId}/book/lore/items/shared-character-id`
  browserDiagnostics.allow(/console.error: Failed to load resource:.*503/)
  browserDiagnostics.allow(/http.5xx: PUT .*\/book\/lore\/items\/shared-character-id returned 503/)
  browserDiagnostics.allow(/console.error: \[LoreWorkspaceTab\] failed to (autosave lore item|flush lore draft)/)
  await page.route(saveURL, route => route.request().method() === 'PUT'
    ? route.fulfill({ status: 503, json: { error: 'Save unavailable' } })
    : route.continue())
  await editor.fill('Comic lore saved on return')
  const rejected = page.waitForResponse(response => response.status() === 503 && response.url().endsWith('/lore/items/shared-character-id'))
  await native.getByRole('button', { name: '返回漫画', exact: true }).click()
  await rejected
  await expect(native.getByRole('status', { name: /自动保存失败/ })).toBeVisible()
  await expect(editor).toContainText('Comic lore saved on return')
  await expect(native).toBeVisible()
  await page.unroute(saveURL)
  await native.getByRole('button', { name: '返回漫画', exact: true }).click()
  await expect(native).toHaveCount(0)
  await expect(studio.getByRole('heading', { name: '第 1 话 / 第 2 页', exact: true })).toBeVisible()
  expect(await frame!.evaluate(node => node === document.querySelector('iframe[title="插件界面"]'))).toBe(true)
  expect(await (await request.get(`/api/projects/${alpha.projectId}/book/lore/items`)).text()).toContain('Comic lore saved on return')
  await expect(agent.getByText('Deterministic E2E response completed.', { exact: true })).toBeVisible()

  await studio.getByRole('button', { name: '设定', exact: true }).click()
  await expect(editor).toBeVisible()
  await page.setViewportSize({ width: 390, height: 844 })
  await page.evaluate(() => { document.documentElement.dataset.theme = 'light' })
  await page.getByRole('button', { name: '收起创作助手', exact: true }).click()
  await expect(editor).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  await page.screenshot({ path: testInfo.outputPath('comic-native-lore-mobile-light.png'), fullPage: true })
  await native.getByRole('button', { name: '在创作 Agent 中引用此设定', exact: true }).click()
  await expect(composer).toBeVisible()
  await page.setViewportSize({ width: 1600, height: 1000 })
  await editor.fill('Comic lore saved on Project switch')
  await page.getByRole('button', { name: /^切换书籍，当前：/ }).click()
  await page.getByRole('menuitem').filter({ hasText: beta.title }).click()
  await expect(page.getByRole('button', { name: `切换书籍，当前：${beta.title}`, exact: true })).toBeVisible()
  expect(await (await request.get(`/api/projects/${alpha.projectId}/book/lore/items`)).text()).toContain('Comic lore saved on Project switch')
  await expect(native).toHaveCount(0)
  await expect(composer).toHaveText('')
  await studio.getByRole('button', { name: '设定', exact: true }).click()
  await expect(native.getByRole('button', { name: /^乙项目角色/ })).toBeVisible()
  await expect(native.getByRole('button', { name: /^甲项目角色/ })).toHaveCount(0)
})

test('comic @ references use the current Book library even when the previous response arrives late', async ({ page, request, browserDiagnostics }, testInfo) => {
  test.skip(!source, 'Set DENOVA_TEST_COMIC_PLUGIN to the index comic plugin directory')
  browserDiagnostics.allow(/console.error: Failed to load resource:.*404.*project-content\/file\?path=comic%2Fcomic.json/)
  browserDiagnostics.allow(/console.error: Failed to load resource:.*404.*project-content\/file\?path=comic%2Fepisodes%2F[0-9a-f-]+%2Fepisode\.json/)
  const alpha = await createAndOpenBook(request, 'Comic lore Alpha')
  const beta = await createAndOpenBook(request, 'Comic lore Beta')
  await installComic(request, alpha.projectId, alpha.workspace)
  await configureComic(request, beta.projectId)
  await createLore(request, alpha.projectId, '甲项目角色', 'COMIC_ALPHA_PRIVATE_BODY')
  await createLore(request, alpha.projectId, '甲项目独有资料', 'COMIC_ALPHA_DRAFT_ONLY_BODY', 'alpha-only-id')
  await createLore(request, beta.projectId, '乙项目角色', 'COMIC_BETA_PRIVATE_BODY')
  expect((await request.post('/api/workspace/switch', { data: { path: alpha.workspace } })).ok()).toBe(true)
  await page.addInitScript(() => localStorage.setItem('nova:mode', 'plugin:index.comic-studio/studio'))
  await page.goto('/')
  const composer = page.locator('#plugin-page-agent').getByRole('textbox')
  await expect(composer).toBeVisible()
  await pickAndSend(page, request, composer, alpha.projectId, '甲项目角色', 'E2E_RUNTIME_COMIC_LORE_ALPHA', 'COMIC_ALPHA_PRIVATE_BODY', 'COMIC_BETA_PRIVATE_BODY')
  await expect(page.locator('#plugin-page-agent').getByText('Deterministic E2E response completed.', { exact: true })).toBeVisible()

  // Leave an unsent token and hold a refresh for Alpha across a real UI switch.
  await composer.fill('@甲项目独有资料')
  await page.getByRole('option', { name: /^@甲项目独有资料/ }).click()
  let release!: () => void
  const gate = new Promise<void>(resolve => { release = resolve })
  let started!: () => void
  const pending = new Promise<void>(resolve => { started = resolve })
  await page.route(`**/api/projects/${alpha.projectId}/book/lore/items`, async route => {
    const response = await route.fetch()
    started()
    await gate
    await route.fulfill({ response })
  })
  try {
    await page.evaluate(projectId => window.dispatchEvent(new CustomEvent('nova:lore-updated', { detail: { projectId } })), alpha.projectId)
    await pending
    await page.getByRole('button', { name: /^切换书籍，当前：/ }).click()
    await page.getByRole('menuitem').filter({ hasText: beta.title }).click()
    await expect(page.getByRole('button', { name: `切换书籍，当前：${beta.title}`, exact: true })).toBeVisible()
    await expect(composer).toBeVisible()
    await expect(composer).toHaveText('')
    await composer.fill('@')
    await expect(page.getByRole('option', { name: /^@乙项目角色/ })).toBeVisible()
    await expect(page.getByRole('option', { name: /^@甲项目角色/ })).toHaveCount(0)
    release()
    await page.unrouteAll({ behavior: 'wait' })
    await expect(page.getByRole('option', { name: /^@乙项目角色/ })).toBeVisible()
    await expect(page.getByRole('option', { name: /^@甲项目角色/ })).toHaveCount(0)
    await page.screenshot({ path: testInfo.outputPath('comic-book-lore.png'), fullPage: true })
    await pickAndSend(page, request, composer, beta.projectId, '乙项目角色', 'E2E_RUNTIME_COMIC_LORE_BETA', 'COMIC_BETA_PRIVATE_BODY', 'COMIC_ALPHA_PRIVATE_BODY')
    await expect(page.locator('#plugin-page-agent').getByText('Deterministic E2E response completed.', { exact: true })).toBeVisible()
    await page.reload()
    await expect(page.locator('#plugin-page-agent').getByText('Deterministic E2E response completed.', { exact: true })).toBeVisible()
  } finally {
    release()
  }
})

test('the comic workspace sends General Project lore into actual model context', async ({ page, request, browserDiagnostics }, testInfo) => {
  test.skip(!source, 'Set DENOVA_TEST_COMIC_PLUGIN to the index comic plugin directory')
  browserDiagnostics.allow(/console.error: Failed to load resource:.*404.*project-content\/file\?path=comic%2Fcomic.json/)
  browserDiagnostics.allow(/console.error: Failed to load resource:.*404.*project-content\/file\?path=comic%2Fepisodes%2F[0-9a-f-]+%2Fepisode\.json/)
  const book = await createAndOpenBook(request, 'Comic host installation')
  await installComic(request, book.projectId, book.workspace)
  const directory = await mkdtemp(path.join(runtimeRoot, 'comic-general-lore-'))
  const project = await registerAgentChatProject(request, directory)
  expect(project.type).toBe('general')
  await configureComic(request, project.id)
  await createLore(request, project.id, '通用项目角色', 'COMIC_GENERAL_PRIVATE_BODY')
  const item = (await (await request.get(`/api/projects/${project.id}/book/lore/items`)).json()).items[0]
  const commented = await request.post(`/api/projects/${project.id}/book/document-comments`, { data: {
    target: { kind: 'lore_item', id: item.id, field: 'content' },
    body: 'General Project character review',
    anchor: { kind: 'text-range', encoding: 'utf8-bytes-v1', revision: item.updated_at, start: 0, end: item.content.length, quote: item.content, display_quote: item.content },
  } })
  expect(commented.ok(), await commented.text()).toBe(true)
  // The fixture mounts the production comic workspace against a real General Project.
  // Book navigation is a separate product surface and is not used to fake its type.
  await page.goto(`/tests/support/plugin-page-agent.html?projectId=${project.id}`)
  const studio = page.frameLocator('iframe[title="插件界面"]')
  await expect(studio.getByText('从第一话开始', { exact: true })).toBeVisible()
  const composer = page.locator('#plugin-page-agent').getByRole('textbox')
  await expect(composer).toBeVisible()
  await page.locator('[data-review-feedback-tray]').getByRole('button').filter({ hasText: 'General Project character review' }).click()
  await expect(page.locator('[data-plugin-host-tab="lore"]').getByRole('textbox', { name: '编辑设定：通用项目角色', exact: true })).toBeVisible()
  await pickAndSend(page, request, composer, project.id, '通用项目角色', 'E2E_RUNTIME_COMIC_LORE_GENERAL', 'COMIC_GENERAL_PRIVATE_BODY', 'COMIC_ALPHA_PRIVATE_BODY')
  await expect(page.getByText('Deterministic E2E response completed.', { exact: true })).toBeVisible()
  const reviewed = await (await request.get(`${modelControlURL}/control/runtime-requests?marker=E2E_RUNTIME_COMIC_LORE_GENERAL`)).json() as E2EModelRequest[]
  expect(JSON.stringify(reviewed[0].messages)).toContain('General Project character review')
  await expect(page.locator('[data-review-feedback-tray]')).toHaveCount(0)
  await composer.fill('@')
  await expect(page.getByRole('option', { name: /^@通用项目角色/ })).toBeVisible()
  await page.screenshot({ path: testInfo.outputPath('comic-general-lore.png'), fullPage: true })
  await page.setViewportSize({ width: 390, height: 844 })
  await page.evaluate(() => { document.documentElement.dataset.theme = 'light' })
  await page.getByRole('button', { name: '展开创作助手', exact: true }).click()
  await composer.fill('@')
  await expect(page.getByRole('option', { name: /^@通用项目角色/ })).toBeInViewport()
  await page.screenshot({ path: testInfo.outputPath('comic-general-lore-mobile-light.png'), fullPage: true })
  await composer.fill('@不存在的资料')
  await expect(page.getByText('未找到文件或资料', { exact: true })).toBeVisible()
})

async function installComic(request: APIRequestContext, projectId: string, workspace: string) {
  await cp(source!, path.join(workspace, 'comic-plugin-source'), { recursive: true, force: false, errorOnExist: true })
  const linked = await request.post('/api/platform/manage/development/link', { data: { kind: 'plugin', projectId, relativePath: 'comic-plugin-source' } })
  expect(linked.ok(), await linked.text()).toBe(true)
  const development = await linked.json()
  const checked = await request.get(`/api/platform/manage/development/${development.developmentId}/check`)
  expect(checked.ok(), await checked.text()).toBe(true)
  const candidate = await checked.json()
  const installed = await request.post('/api/platform/manage/packages/install', { data: { candidateId: candidate.candidateId, grants: candidate.manifest.permissions.required } })
  expect(installed.ok(), await installed.text()).toBe(true)
  await enablePlugin(request, candidate.manifest.id)
  await configureComic(request, projectId)
}

async function configureComic(request: APIRequestContext, projectId: string) {
  const config = await (await request.get(`/api/platform/manage/projects/${projectId}/extensions`)).json()
  const response = await request.put(`/api/platform/manage/projects/${projectId}/extensions`, { data: { expectedRevision: config.revision, extensions: { ...config.extensions, models: { 'index.comic-studio/image': 'e2e-image' } } } })
  expect(response.ok(), await response.text()).toBe(true)
}

async function createLore(request: APIRequestContext, projectId: string, name: string, content: string, id = 'shared-character-id') {
  const response = await request.post(`/api/projects/${projectId}/book/lore/items`, { data: { id, name, type: 'character', content, enabled: true } })
  expect(response.ok(), await response.text()).toBe(true)
}

async function pickAndSend(page: Page, request: APIRequestContext, composer: Locator, projectId: string, name: string, marker: string, body: string, foreignBody: string) {
  await composer.fill(`@${name}`)
  const reference = page.getByRole('option', { name: new RegExp(`^@${name}`) })
  await expect(reference).toBeVisible()
  await reference.click()
  await composer.pressSequentially(` 请参考这份角色资料。 ${marker}`)
  const submitted = page.waitForRequest(req => req.method() === 'POST' && req.url().endsWith(`/api/projects/${projectId}/agent-chat/chat`))
  await page.locator('[data-action="send"]').filter({ visible: true }).click()
  expect((await submitted).postDataJSON().lore_references).toEqual(['shared-character-id'])
  await expect.poll(async () => {
    const response = await request.get(`${modelControlURL}/control/runtime-requests?marker=${marker}`)
    expect(response.ok()).toBe(true)
    return (await response.json()).length
  }).toBeGreaterThan(0)
  const captured = await (await request.get(`${modelControlURL}/control/runtime-requests?marker=${marker}`)).json() as E2EModelRequest[]
  const input = JSON.stringify(captured[0].messages)
  expect(input).toContain(body)
  expect(input).toContain('ID: shared-character-id')
  expect(input).not.toContain(foreignBody)
}
