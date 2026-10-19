import { expect, test } from '../support/fixtures'
import { createAndOpenBook, createStartedStory } from '../support/api'
import { submitAgentChatMessage } from '../support/agent-chat'

for (const mode of ['writing', 'game'] as const) {
  test(`configuration manages the shared lore index from ${mode}`, async ({ page, request }, testInfo) => {
    const book = await createAndOpenBook(request, `Index configuration ${mode}`)
    if (mode === 'game') await createStartedStory(request, 'Index configuration game')
    const base = `/api/projects/${book.projectId}/book/lore`
    const index = await (await request.get(`${base}/index`)).json()
    const originalGroup = { id: 'retained', name: '原有分组', purpose: 'Preserved purpose', body_markdown: 'RETAINED_NOTES', default_detail: 'name' }
    expect((await request.put(`${base}/index`, { data: { base_revision: index.revision, guide: {
      intro_markdown: '', groups: [originalGroup], automatic_details: { resident: 'brief' },
    } } })).ok()).toBe(true)
    for (const data of [
      { id: 'captain', name: '船长', type: 'character', content: 'E2E_CONFIG_CAPTAIN_BODY', brief_description: 'Captain brief', load_mode: 'auto', index_memberships: [{ group_id: 'retained', detail: 'name' }] },
      { id: 'harbor', name: '码头', type: 'location', content: 'E2E_CONFIG_HARBOR_BODY', brief_description: 'E2E_CONFIG_HARBOR_BRIEF', load_mode: 'auto' },
    ]) expect((await request.post(`${base}/items`, { data })).ok()).toBe(true)
    const settings = await (await request.get('/api/settings')).json()
    expect((await request.patch('/api/settings', { data: { layer: 'user', base_revision: settings.revisions.user, changes: {
      theme: mode === 'writing' ? 'dark' : 'light', language: 'zh-CN',
    } } })).ok()).toBe(true)
    await page.setViewportSize({ width: 1680, height: 1000 })
    await page.goto('/')
    const sidebar = page.getByLabel('工作台侧边栏')
    await sidebar.getByRole('button', { name: mode === 'writing' ? '写作' : '游戏', exact: true }).click()
    await sidebar.getByRole('button', { name: '资料库', exact: true }).click()
    await page.getByTestId('lore-library').getByRole('button', { name: '更多', exact: true }).click()
    await page.getByRole('menuitem', { name: '资料索引', exact: true }).click()
    const doc = page.getByTestId('lore-index-document')
    await expect(doc).toBeVisible()
    const composer = page.getByPlaceholder(/输入消息/).filter({ visible: true })
    if (await composer.count() === 0) await page.getByRole('button', { name: '配置管理', exact: true }).click()
    await expect(composer).toBeVisible()
    await submitAgentChatMessage(page, composer, 'E2E_LORE_INDEX_CONFIG 请整理资料索引，添加港口人物分组并关联船长。')
    await expect(page.getByText('E2E index configuration verified.', { exact: true })).toBeVisible()
    await expect(doc.getByTestId('lore-index-section')).toHaveCount(2)
    const saved = await (await request.get(`${base}/index`)).json()
    expect(saved.guide.groups[0]).toEqual(originalGroup)
    expect(saved.guide.intro_markdown).toBe('E2E_CONFIG_READING_GUIDE')
    expect(saved.guide.automatic_details).toEqual({ resident: 'brief', 'auto:location': 'brief' })
    const { items } = await (await request.get(`${base}/items`)).json()
    const captain = items.find((item: { id: string }) => item.id === 'captain')
    expect(captain.content).toBe('E2E_CONFIG_CAPTAIN_BODY')
    expect(captain.index_memberships).toEqual([{ group_id: 'retained', detail: 'name' }, { group_id: 'port-cast', detail: 'inherit' }])
    // Close the chat to inspect the live projection, then verify persistence.
    await page.getByRole('button', { name: '配置管理', exact: true }).click()
    const preview = doc.getByTestId('lore-index-preview')
    await expect(preview).toContainText('E2E_CONFIG_CAPTAIN_BODY')
    await expect(preview).toContainText('E2E_CONFIG_HARBOR_BRIEF')
    await expect(preview).not.toContainText('E2E_CONFIG_HARBOR_BODY')
    await page.screenshot({ path: testInfo.outputPath(`index-config-${mode}.png`) })
    await page.reload()
    await page.getByRole('button', { name: '资料索引', exact: true }).click()
    await expect(page.getByTestId('lore-index-document').getByTestId('lore-index-section')).toHaveCount(2)
  })
}
