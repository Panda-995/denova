import { randomUUID } from 'node:crypto'
import { expect, test } from '../support/fixtures'
import { createAndOpenBook } from '../support/api'

for (const theme of ['dark', 'light']) {
  test(`book recommendations allow independent story resource choices in ${theme}`, async ({ page, request }) => {
    await request.patch('/api/settings', { data: { layer: 'user', changes: { language: 'zh-CN', theme } } })
    const book = await createAndOpenBook(request, 'Independent story resources')
    const protagonist = await request.post(`/api/projects/${book.projectId}/book/lore/items`, { data: {
      id: 'hero', name: 'Player', type: 'character', content: 'A traveler exploring an old station.', tags: ['主角'],
    } })
    expect(protagonist.ok(), await protagonist.text()).toBe(true)
    const state = await (await request.get('/api/actor-states/default')).json()
    state.id = `chosen-state-${randomUUID()}`
    state.name = `Player chosen state ${theme}`
    state.actor_state.templates[0].name = 'Player chosen template'
    const createdState = await request.post('/api/actor-states', { data: state })
    expect(createdState.ok(), await createdState.text()).toBe(true)
    const rules = await request.post('/api/rule-systems', { data: {
      id: `recommended-rules-${randomUUID()}`, name: 'Recommended rules', actor_state_id: 'default',
    } })
    expect(rules.ok(), await rules.text()).toBe(true)
    const rule = await rules.json()
    const defaults = { actor_state_id: 'default', rule_system_id: rule.id, event_package_ids: [] }
    const settingsURL = `/api/projects/${book.projectId}/settings`
    const saved = await request.patch(settingsURL, { data: { layer: 'workspace', changes: { game_creation_defaults: defaults } } })
    expect(saved.ok(), await saved.text()).toBe(true)

    await page.addInitScript(() => localStorage.setItem('nova:mode', 'interactive'))
    await page.goto('/')
    await page.getByRole('button', { name: '新建', exact: true }).filter({ visible: true }).click()
    await expect(page.getByText('已使用本书默认资源；成功创建后会记住本次配置。')).toBeVisible()
    await page.getByRole('tab', { name: 'AI 生成', exact: true }).click()
    await page.getByRole('combobox', { name: '状态结构', exact: true }).click()
    await page.getByRole('option', { name: '固定使用模板', exact: true }).click()
    await page.getByRole('combobox', { name: '角色状态系统', exact: true }).click()
    await page.getByRole('option', { name: state.name, exact: true }).click()
    const choiceCount = page.getByRole('spinbutton', { name: '候选行动数', exact: true })
    await choiceCount.fill('2')
    await choiceCount.press('Enter')
    const creating = page.waitForResponse(response => response.request().method() === 'POST' && new URL(response.url()).pathname === '/api/interactive/stories')
    await page.getByRole('button', { name: '开始故事', exact: true }).click()
    const created = await creating
    expect(created.ok(), await created.text()).toBe(true)
    const story = await created.json()
    expect(story.module_refs).toMatchObject({ actor_state_id: state.id, rule_system_id: rule.id })
    const snapshotResponse = await request.get(`/api/interactive/stories/${story.id}/snapshot?branch=main`)
    expect(snapshotResponse.ok(), await snapshotResponse.text()).toBe(true)
    const snapshot = await snapshotResponse.json()
    expect(snapshot.actor_state_schema.system.templates[0].name).toBe('Player chosen template')
    expect((await (await request.get(settingsURL)).json()).workspace.game_creation_defaults).toEqual(defaults)
    await expect.poll(async () => (await (await request.get(`/api/interactive/stories/${story.id}/snapshot?branch=main`)).json()).turn_count).toBe(1)

    // Reload the canonical projection and verify that resource resolution keeps the choice.
    await page.reload()
    const stories = (await (await request.get('/api/interactive/stories')).json()).stories
    expect(stories.find((item: { id: string }) => item.id === story.id).module_refs.actor_state_id).toBe(state.id)

    const generated = await request.post('/api/interactive/stories', { data: {
      module_refs: { rule_system_id: rule.id, actor_state_disabled: true },
      state_schema_policy: { mode: 'generate' },
    } })
    expect(generated.ok(), await generated.text()).toBe(true)
    expect((await generated.json()).module_refs).toMatchObject({ rule_system_id: rule.id, actor_state_disabled: true })
  })
}

test('unavailable book recommendations do not reject explicit story resources', async ({ request }) => {
  const book = await createAndOpenBook(request, 'Unavailable recommendations')
  const saved = await request.patch(`/api/projects/${book.projectId}/settings`, { data: { layer: 'workspace', changes: {
    game_creation_defaults: { narrative_style_id: 'missing', image_preset_id: 'missing', actor_state_id: 'missing', rule_system_id: 'missing', event_package_ids: ['missing'], planning_template_id: 'missing' },
  } } })
  expect(saved.ok(), await saved.text()).toBe(true)
  const created = await request.post('/api/interactive/stories', { data: {
    module_refs: { narrative_style_id: 'classic', image_preset_id: 'game-cg', actor_state_id: 'default', rule_system_id: 'default', event_packages_disabled: true },
    planning_template_id: 'default', state_schema_policy: { mode: 'fixed_template' },
  } })
  expect(created.ok(), await created.text()).toBe(true)
  expect((await created.json()).module_refs).toMatchObject({ narrative_style_id: 'classic', actor_state_id: 'default', rule_system_id: 'default', event_packages_disabled: true })

  const inherited = await request.post('/api/interactive/stories', { data: { state_schema_policy: { mode: 'fixed_template' } } })
  expect(inherited.ok(), await inherited.text()).toBe(true)
  const story = await inherited.json()
  expect(story.module_refs).toMatchObject({ narrative_style_id: 'missing', image_preset_id: 'missing', actor_state_id: 'missing', rule_system_id: 'missing', event_package_ids: ['missing'] })
})
