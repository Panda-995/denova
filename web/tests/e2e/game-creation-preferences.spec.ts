import { expect, test } from '../support/fixtures'
import { createAndOpenBook } from '../support/api'

for (const [theme, locale] of [['dark', 'zh-CN'], ['light', 'en-US']] as const) {
  test(`remembers successful creation per book and resets only the draft in ${theme} ${locale}`, async ({ page, request }) => {
    const zh = locale === 'zh-CN'
    await request.patch('/api/settings', { data: { layer: 'user', changes: { language: locale, theme } } })
    const book = await createAndOpenBook(request, 'Creation preferences')
    const settingsURL = `/api/projects/${book.projectId}/settings`
    const readSettings = async () => (await (await request.get(settingsURL)).json()).workspace
    expect((await readSettings()).game_last_creation).toBeUndefined()
    const hero = await request.post(`/api/projects/${book.projectId}/book/lore/items`, { data: {
      id: 'hero', name: 'Book protagonist', type: 'character', content: 'A traveler at an old station.', tags: ['主角'],
    } })
    expect(hero.ok(), await hero.text()).toBe(true)
    const companion = await request.post(`/api/projects/${book.projectId}/book/lore/items`, { data: {
      id: 'companion', name: 'Remembered companion', type: 'character', content: 'An engineer traveling with the protagonist.', tags: ['同伴'],
    } })
    expect(companion.ok(), await companion.text()).toBe(true)

    const defaults = { narrative_style_id: 'classic', planning_template_id: 'default', event_package_ids: [], default_background: { mode: 'none' } }
    const saved = await request.patch(settingsURL, { data: { layer: 'workspace', changes: { game_creation_defaults: defaults } } })
    expect(saved.ok(), await saved.text()).toBe(true)
    const input = {
      title: 'Original creation', custom_agent_id: '',
      protagonist: { mode: 'custom', name: 'Previous protagonist', profile: 'A traveler searching for a lost friend.' },
      opening: { mode: 'custom', custom_text: 'Previous opening content.' },
      planning_template_id: 'directed-longform', planning_mode: 'disabled', reply_target_chars: 1200, choice_count: 4,
      module_refs: { narrative_style_id: 'classic', actor_state_id: 'default', rule_system_id: 'default', image_preset_id: 'game-cg', event_packages_disabled: true },
      state_schema_policy: { mode: 'fixed_template' },
      image_settings: { mode: 'manual', interval_turns: 7, preset_id: 'game-cg' },
      check_settings: { difficulty_shift: 1, roll_modifier: 3 },
      presentation_settings: { background: false, characters: false },
    }
    const created = await request.post('/api/interactive/stories', { data: input })
    expect(created.ok(), await created.text()).toBe(true)
    const first = await created.json()
    const remembered = (await readSettings()).game_last_creation
    expect(remembered).toMatchObject({
      planning_enabled: false, reply_target_chars: 1200, choice_count: 4, image_interval_turns: 7,
      difficulty_shift: 1, roll_modifier: 3, state_schema_mode: 'fixed_template', background: false, characters: false,
      resources: { ...defaults, planning_template_id: 'directed-longform' },
      protagonist: input.protagonist,
    })
    expect(remembered).not.toHaveProperty('opening')

    // A failed creation and later tuning of an existing Story do not change the seed.
    const failed = await request.post('/api/interactive/stories', { data: { ...input, planning_mode: 'invalid' } })
    expect(failed.status()).toBe(400)
    const tuned = await request.patch(`/api/interactive/stories/${first.id}`, { data: { choice_count: 8, reply_target_chars: 2400 } })
    expect(tuned.ok(), await tuned.text()).toBe(true)
    expect((await readSettings()).game_last_creation).toEqual(remembered)

    await page.addInitScript(locale => {
      localStorage.setItem('nova:mode', 'interactive')
      localStorage.setItem('nova.locale.configured', locale)
    }, locale)
    await page.goto('/')
    const openNew = async () => {
      await page.getByRole('button', { name: zh ? '新建' : 'New', exact: true }).filter({ visible: true }).click()
      const advanced = page.getByRole('button', { name: zh ? '高级设置' : 'Advanced settings', exact: true })
      if (await advanced.getAttribute('aria-expanded') === 'false') await advanced.click()
      await expect(page.getByTestId('story-creation-preferences')).toBeVisible()
    }
    await openNew()
    const choice = page.getByRole('spinbutton', { name: zh ? '候选行动数' : 'Suggested actions', exact: true })
    const length = page.getByRole('button', { name: zh ? '每回合目标字数' : 'Target characters per turn', exact: true })
    const background = page.getByRole('switch', { name: zh ? '动态背景' : 'Dynamic backgrounds', exact: true })
    const reset = page.getByRole('button', { name: zh ? '恢复默认设置' : 'Restore default settings', exact: true })
    const saveDefaults = page.getByRole('button', { name: zh ? '保存资源搭配为本书默认' : 'Save resource choices as book defaults', exact: true })
    await expect(choice).toHaveValue('4')
    await expect(length).toHaveText('1200')
    await expect(background).not.toBeChecked()
    await expect(page.getByTestId('story-creation-preferences')).toContainText(zh ? '已沿用本书上次成功创建时的配置。' : 'Using the settings from this book’s last successful creation.')
    const opening = page.getByPlaceholder(zh ? '可选。写下必须保留的场景、人物处境或第一事件；留空则自动生成。' : 'Optional. Write a scene, character situation, or first event to preserve, or leave this blank to generate the opening.')
    await expect(opening).toHaveValue('')
    await expect(page.getByRole('radio', { name: zh ? '自定义角色' : 'Custom character', exact: true })).toBeChecked()

    const name = page.getByRole('textbox', { name: zh ? '角色名称' : 'Character name', exact: true })
    await expect(name).toHaveValue(input.protagonist.name)
    await expect(page.getByRole('textbox', { name: zh ? '角色设定' : 'Character profile', exact: true })).toHaveValue(input.protagonist.profile)
    await name.fill('Current protagonist')
    const customOpening = page.locator('textarea[placeholder]').last()
    const longOpening = 'A new opening that must survive reset. '.repeat(40)
    await customOpening.fill(longOpening)
    const model = page.getByRole('combobox', { name: zh ? '模型配置' : 'Model configuration', exact: true })
    await expect(model).toBeEnabled()
    const modelBefore = await model.textContent()
    await reset.click()
    await expect(choice).toHaveValue('5')
    await expect(length).toHaveText('2000')
    await expect(background).toBeChecked()
    await expect(name).toHaveValue('Current protagonist')
    await expect(customOpening).toHaveValue(longOpening)
    await expect(model).toHaveText(modelBefore!)
    expect((await readSettings()).game_last_creation).toEqual(remembered)

    // Saving a reviewed default updates the reset target without changing memory.
    const planning = page.getByRole('combobox', { name: zh ? '游戏规划' : 'Game Planning', exact: true })
    await planning.click()
    await page.getByRole('option', { name: zh ? '角色与关系' : /Character/ }).click()
    const planningName = await planning.textContent()
    await saveDefaults.click()
    const dialog = page.getByRole('dialog', { name: zh ? '本书的新故事默认配置' : 'New-story defaults for this book', exact: true })
    await dialog.getByRole('checkbox', { name: zh ? /^规划方案/ : /^Planning template/ }).check()
    await dialog.getByRole('button', { name: zh ? '保存' : 'Save', exact: true }).click()
    await expect(dialog).toBeHidden()
    const updatedDefaults = {
      ...defaults, planning_template_id: 'character-relationships',
      actor_state_id: 'default', image_preset_id: 'game-cg', rule_system_id: 'default',
    }
    expect((await readSettings()).game_creation_defaults).toEqual(updatedDefaults)
    expect((await readSettings()).game_last_creation).toEqual(remembered)
    await reset.click()
    await expect(planning).toHaveText(planningName!)

    for (const width of [1440, 390]) {
      await page.setViewportSize({ width, height: 900 })
      await reset.scrollIntoViewIfNeeded()
      expect(await page.getByTestId('story-creation-preferences').evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true)
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
      await page.screenshot({ path: test.info().outputPath(`creation-preferences-${theme}-${width}.png`) })
    }
    await page.getByRole('button', { name: zh ? '高级设置' : 'Advanced settings', exact: true }).click()
    await expect(reset).toBeHidden()
    await expect(saveDefaults).toBeHidden()
    await page.getByRole('button', { name: zh ? '取消' : 'Cancel', exact: true }).click()
    await page.setViewportSize({ width: 1440, height: 900 })
    await openNew()
    await expect(choice).toHaveValue('4')
    await expect(name).toHaveValue(input.protagonist.name)

    // A subsequent successful UI creation replaces the remembered snapshot.
    await page.getByRole('radio', { name: zh ? '从资料库选择' : 'Choose from Lore', exact: true }).check()
    await page.getByRole('button', { name: zh ? '更换' : 'Change', exact: true }).click()
    await page.getByRole('option', { name: /Remembered companion/ }).click()
    await choice.fill('2')
    await choice.press('Enter')
    const creating = page.waitForResponse(response => response.request().method() === 'POST' && new URL(response.url()).pathname === '/api/interactive/stories')
    const openingImageCheck = page.waitForResponse(response => response.request().method() === 'POST' && new URL(response.url()).pathname.endsWith('/images/generate'))
    await page.getByRole('button', { name: zh ? '开始故事' : 'Start story', exact: true }).click()
    const response = await creating
    expect(response.ok(), await response.text()).toBe(true)
    const second = await response.json()
    expect(second).toMatchObject({
      planning_template_id: input.planning_template_id, planning_mode: 'disabled', reply_target_chars: 1200, choice_count: 2,
      module_refs: input.module_refs, image_settings: input.image_settings, check_settings: input.check_settings,
      presentation_settings: input.presentation_settings, state_schema_policy: input.state_schema_policy,
      protagonist: { mode: 'lore', source_lore_item_id: 'companion' }, opening: { mode: 'ai' },
    })
    const secondRemembered = { ...remembered, choice_count: 2, protagonist: { mode: 'lore', source_lore_item_id: 'companion' } }
    expect((await readSettings()).game_last_creation).toEqual(secondRemembered)
    expect((await readSettings()).game_creation_defaults).toEqual(updatedDefaults)
    await expect.poll(async () => (await (await request.get(`/api/interactive/stories/${second.id}/snapshot?branch=main`)).json()).turn_count).toBe(1)
    // The opening finishes its image-policy check after the turn is durable.
    // Let that request settle before switching the server's active workspace.
    const imageCheck = await openingImageCheck
    expect(imageCheck.ok(), await imageCheck.text()).toBe(true)

    // Switching books and deleting the source Story must not lose or leak preferences.
    const other = await createAndOpenBook(request, 'Independent preferences')
    await page.reload()
    await openNew()
    await expect(choice).toHaveValue('5')
    await expect(background).toBeChecked()
    expect((await (await request.get(`/api/projects/${other.projectId}/settings`)).json()).workspace.game_last_creation).toBeUndefined()
    await request.post('/api/workspace/switch', { data: { path: book.workspace } })
    const deleted = await request.delete(`/api/interactive/stories/${second.id}`)
    expect(deleted.ok(), await deleted.text()).toBe(true)
    await page.reload()
    await openNew()
    await expect(choice).toHaveValue('2')
    await expect(page.getByText('Remembered companion', { exact: true })).toBeVisible()

    // An unavailable remembered character falls back to the tagged character.
    const removed = await request.delete(`/api/projects/${book.projectId}/book/lore/items/companion`)
    expect(removed.ok(), await removed.text()).toBe(true)
    await page.reload()
    await openNew()
    await expect(page.getByText('Book protagonist', { exact: true })).toBeVisible()
    expect((await readSettings()).game_last_creation).toEqual(secondRemembered)
  })
}
