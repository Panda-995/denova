import { expect, test } from '../support/fixtures'
import { createAndOpenBook, createStartedStory } from '../support/api'

for (const theme of ['dark', 'light']) {
  test(`user character layout persists across projects and adapts in ${theme}`, async ({ page, request }) => {
    test.setTimeout(120_000)
    await request.patch('/api/settings', { data: { layer: 'user', changes: { theme, language: 'zh-CN', interactive_stage_character_layout: 'center', interactive_stage_character_size: null, interactive_stage_scrim_opacity: 0.25 } } })
    const book = await createAndOpenBook(request, `Character layout ${theme}`)
    await createStartedStory(request, '角色布局验证')
    let castCount = 3
    // Rendering fixtures exercise arbitrary cast sizes without depending on model image selection.
    await page.route('**/api/**/snapshot*', async route => {
      const response = await route.fetch()
      const snapshot = await response.json()
      const characters = Array.from({ length: castCount }, (_, i) => ({ item_id: `cast-${i}`, asset_id: `sprite-${i}`, path: `sprite-${i}.svg`, name: 'Long character name '.repeat(10) }))
      for (const turn of [snapshot.current_turn, ...(snapshot.turns || [])]) {
        if (turn?.turn_result) turn.turn_result.presentation = { characters }
      }
      await route.fulfill({ response, json: snapshot })
    })
    await page.route('**/*sprite-*.svg*', route => route.fulfill({ contentType: 'image/svg+xml', body: '<svg xmlns="http://www.w3.org/2000/svg" width="180" height="420"><circle cx="90" cy="70" r="55" fill="#c69b69"/><path d="M45 140H135L175 420H5Z" fill="#708ca0"/></svg>' }))
    await page.setViewportSize({ width: 1800, height: 1000 })
    await page.goto('/')
    await page.getByLabel('工作台侧边栏').getByRole('button', { name: '游戏', exact: true }).click()
    await page.getByRole('tab', { name: '控制', exact: true }).click()
    const layout = page.getByRole('radiogroup', { name: '角色布局', exact: true })
    await expect(layout).toHaveCount(1)
    await expect(layout.getByRole('radio')).toHaveCount(4)
    const size = page.getByRole('slider', { name: '角色大小', exact: true })
    await expect(size).toHaveAttribute('aria-valuetext', '70%')
    const panel = page.locator('section').filter({ has: page.getByRole('heading', { name: '舞台演出', exact: true }) }).last()
    await expect(panel.locator('[data-slot="field-description"]')).toHaveCount(0)
    const cast = page.locator('.nova-stage-characters')
    await expect(cast.locator('img')).toHaveCount(3)
    // Measure the contained artwork, not its box: object-fit can hide shrinking.
    const spriteHeights = () => cast.locator('img').evaluateAll(images => images.map(image => {
      const sprite = image as HTMLImageElement
      const box = sprite.getBoundingClientRect()
      return Math.min(box.height, box.width * sprite.naturalHeight / sprite.naturalWidth)
    }))
    const stageHeight = (await cast.boundingBox())!.height
    await expect.poll(spriteHeights).toEqual(Array(3).fill(stageHeight * 0.7).map(value => expect.closeTo(value, 0)))
    await layout.getByRole('radio', { name: '靠左', exact: true }).click()
    await expect(cast).toHaveAttribute('data-layout', 'left')
    await expect.poll(spriteHeights).toEqual(Array(3).fill(stageHeight * 0.7).map(value => expect.closeTo(value, 0)))
    await layout.getByRole('radio', { name: '两侧', exact: true }).click()
    await expect(cast).toHaveAttribute('data-layout', 'sides')
    const settingsURL = `/api/projects/${book.projectId}/settings`
    await expect.poll(async () => (await (await request.get(settingsURL)).json()).user.interactive_stage_character_layout).toBe('sides')
    expect((await (await request.get(settingsURL)).json()).workspace.interactive_stage_character_layout).toBeUndefined()
    await page.reload()
    await expect(cast).toHaveAttribute('data-layout', 'sides')
    await expect(cast.locator('img')).toHaveCount(3)
    const stage = (await cast.boundingBox())!
    const first = (await cast.locator('.nova-stage-character').nth(0).boundingBox())!
    const second = (await cast.locator('.nova-stage-character').nth(1).boundingBox())!
    expect(first.x + first.width).toBeLessThan(stage.x + stage.width / 2)
    expect(second.x).toBeGreaterThan(stage.x + stage.width / 2)
    // Crowds keep the same size in every layout, and remain inside the stage.
    for (const count of [6, 16]) {
      castCount = count
      await page.reload()
      await expect(cast.locator('img')).toHaveCount(count)
      for (const label of ['居中', '靠左', '靠右', '两侧']) {
        await layout.getByRole('radio', { name: label, exact: true }).click()
        await expect.poll(spriteHeights).toEqual(Array(count).fill(stageHeight * 0.7).map(value => expect.closeTo(value, 0)))
        for (const sprite of await cast.locator('img').all()) {
          const box = (await sprite.boundingBox())!
          expect(box.x).toBeGreaterThanOrEqual(stage.x - 1)
          expect(box.x + box.width).toBeLessThanOrEqual(stage.x + stage.width + 1)
        }
      }
    }
    castCount = 3
    await page.reload()
    await expect(cast.locator('img')).toHaveCount(3)
    await layout.scrollIntoViewIfNeeded()
    await page.screenshot({ path: test.info().outputPath(`characters-${theme}-wide.png`) })
    await page.setViewportSize({ width: 390, height: 844 })
    await expect.poll(async () => {
      const height = (await cast.boundingBox())!.height
      return (await spriteHeights()).every(spriteHeight => spriteHeight >= height * 0.69)
    }).toBe(true)
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
    await page.screenshot({ path: test.info().outputPath(`characters-${theme}-narrow.png`) })
    await page.getByRole('button', { name: '显示控制台', exact: true }).click()
    const consolePanel = page.getByRole('dialog', { name: '控制台', exact: true })
    await consolePanel.getByRole('tab', { name: '控制', exact: true }).click()
    await layout.scrollIntoViewIfNeeded()
    await expect(layout.getByRole('radio', { name: '两侧', exact: true })).toBeVisible()
    await expect(size).toBeVisible()
    await page.screenshot({ path: test.info().outputPath(`character-controls-${theme}-narrow.png`) })
    await page.setViewportSize({ width: 1800, height: 1000 })
    await expect(cast.locator('.nova-stage-character').first()).toHaveCSS('position', 'absolute')
    castCount = 1
    await page.reload()
    await expect(cast.locator('img')).toHaveCount(1)
    await expect(cast.locator('.nova-stage-character')).toHaveAttribute('data-side', 'right')
    await size.press('Home')
    await expect(size).toHaveAttribute('aria-valuetext', '40%')
    await expect.poll(spriteHeights).toEqual([expect.closeTo(stageHeight * 0.4, 0)])
    await size.press('End')
    await expect(size).toHaveAttribute('aria-valuetext', '100%')
    await expect.poll(spriteHeights).toEqual([expect.closeTo(stageHeight, 0)])
    await page.reload()
    await expect(size).toHaveAttribute('aria-valuetext', '100%')
    await layout.getByRole('radio', { name: '靠右', exact: true }).click()
    await expect(cast).toHaveAttribute('data-layout', 'right')
    castCount = 0
    await page.reload()
    await expect(page.getByTestId('story-stage-artwork')).toHaveCount(0)
    await expect(layout).toBeVisible()

    // API fixture setup switches the foreground Project outside the UI. Close
    // the old page first so its pending configuration reads retain their owner.
    await page.goto('about:blank')
    const secondBook = await createAndOpenBook(request, `Shared character layout ${theme}`)
    await createStartedStory(request, '共享用户布局')
    castCount = 2
    await page.goto('/')
    await expect(cast).toHaveAttribute('data-layout', 'right')
    await page.getByRole('tab', { name: '控制', exact: true }).click()
    await expect(layout.getByRole('radio', { name: '靠右', exact: true })).toBeChecked()
    await expect(size).toHaveAttribute('aria-valuetext', '100%')
    await layout.getByRole('radio', { name: '居中', exact: true }).click()
    await expect(cast).toHaveAttribute('data-layout', 'center')
    for (const projectId of [book.projectId, secondBook.projectId]) {
      const settings = await (await request.get(`/api/projects/${projectId}/settings`)).json()
      expect(settings.user.interactive_stage_character_layout).toBe('center')
      expect(settings.effective.interactive_stage_character_layout).toBe('center')
      expect(settings.workspace.interactive_stage_character_layout).toBeUndefined()
      expect(settings.user.interactive_stage_character_size).toBe(1)
      expect(settings.effective.interactive_stage_character_size).toBe(1)
      expect(settings.workspace.interactive_stage_character_size).toBeUndefined()
    }
    if (theme === 'light') {
      await request.patch('/api/settings', { data: { layer: 'user', changes: { language: 'en-US' } } })
      await page.reload()
      const englishLayout = page.getByRole('radiogroup', { name: 'Character layout', exact: true })
      await expect(englishLayout.getByRole('radio', { name: 'Center', exact: true })).toBeChecked()
      await expect(page.getByRole('slider', { name: 'Character size', exact: true })).toHaveAttribute('aria-valuetext', '100%')
      await englishLayout.scrollIntoViewIfNeeded()
      await page.screenshot({ path: test.info().outputPath('character-controls-english.png') })
    }
  })
}
