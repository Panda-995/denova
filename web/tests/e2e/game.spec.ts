import { expect, test } from '../support/fixtures'
import { createAndOpenBook, createStartedStory, getStoryBranches, getStorySnapshot } from '../support/api'
import { submitAgentChatMessage } from '../support/agent-chat'
import { allowGameRegeneration, getModelStatus, releaseDelayedRequest } from '../support/model'

const gameOpeningNarrative = '暮色落在旧车站外，石门后的轨道传来遥远的回声。'
const gameFollowUpDelayMarker = 'E2E_GAME_FOLLOW_UP_DELAY'
const gameFollowUpMarker = 'E2E_GAME_FOLLOW_UP_STEER'
const gameFollowUpNarrative = '你立即改变方向，沿着新发现的脚印进入旧车站。'
const gameBranchPlanMarker = 'E2E_GAME_BRANCH_PLAN'

for (const theme of ['dark', 'light'] as const) {
  test(`persists Game text width and aligns responsive reading layout in ${theme}`, async ({ page, request }) => {
    await createAndOpenBook(request, `Game reading width ${theme}`)
    const story = await createStartedStory(request, 'Reading width')
    const turn = (await getStorySnapshot(request, story.id)).turns[0]
    const narrative = Array.from({ length: 12 }, (_, index) => `段落 ${index + 1}：${'车站的灯光照亮漫长的轨道，旅人沿着脚印继续探索。'.repeat(8)}`).join('\n\n')
    const edited = await request.patch(`/api/interactive/stories/${story.id}/turns/${turn.id}/narrative`, {
      data: { branch_id: 'main', expected_narrative: turn.narrative, narrative },
    })
    expect(edited.ok(), await edited.text()).toBe(true)
    const settings = await request.patch('/api/settings', { data: { layer: 'user', changes: { theme, interactive_stage_text_max_width: null } } })
    expect(settings.ok(), await settings.text()).toBe(true)
    await page.setViewportSize({ width: 2200, height: 1000 })
    await page.goto('/')
    await page.getByLabel('工作台侧边栏').getByRole('button', { name: '游戏', exact: true }).click()
    const row = page.locator('[data-nova-chat-item].nova-story-reading-column').last()
    const composerColumn = page.locator('.nova-story-input-float > .nova-story-reading-column')
    await expect(row).toBeVisible()
    await expect.poll(async () => (await row.boundingBox())?.width).toBe(896)
    await page.getByRole('tab', { name: '控制', exact: true }).click()
    const width = page.getByRole('spinbutton', { name: '文本最大宽度（px）' })
    await expect(width).toHaveValue('896')
    const timeline = page.locator('.nova-story-stage-content .nova-chat-canvas')
    await timeline.hover()
    await page.mouse.wheel(0, -700)
    await expect.poll(() => timeline.evaluate(el => el.scrollHeight - el.clientHeight - el.scrollTop)).toBeGreaterThan(500)
    await width.fill('720')
    await width.press('Enter')
    await expect.poll(async () => (await (await request.get('/api/settings')).json()).user.interactive_stage_text_max_width).toBe(720)
    await expect.poll(async () => (await row.boundingBox())?.width).toBe(720)
    await expect(width).toHaveValue('720')
    await expect(width).toBeEnabled()
    // Reflow must not pull a reader who scrolled into history back to the bottom.
    await expect.poll(() => timeline.evaluate(el => el.scrollHeight - el.clientHeight - el.scrollTop)).toBeGreaterThan(500)
    await page.reload()
    await expect.poll(async () => (await composerColumn.boundingBox())?.width).toBe(720)
    await page.getByRole('button', { name: '获取行动选择' }).click()
    await expect(page.getByText('走进旧车站', { exact: true })).toBeVisible()
    for (const viewport of [{ width: 2200, height: 1000 }, { width: 390, height: 844 }, { width: 700, height: 900 }]) {
      await page.setViewportSize(viewport)
      await expect(composerColumn).toBeVisible()
      await expect.poll(async () => {
        const message = (await row.boundingBox())!
        const composer = (await composerColumn.boundingBox())!
        return Math.abs(message.x - composer.x) + Math.abs(message.width - composer.width)
      }).toBeLessThan(2)
      const box = (await composerColumn.boundingBox())!
      expect(box.width).toBeLessThanOrEqual(720)
      expect(box.x).toBeGreaterThanOrEqual(0)
      expect(box.x + box.width).toBeLessThanOrEqual(viewport.width)
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
      await page.getByPlaceholder(/你要做什么/).focus()
      await expect(page.getByText('走进旧车站', { exact: true })).toBeVisible()
      await page.screenshot({ path: test.info().outputPath(`game-width-${theme}-${viewport.width}.png`) })
    }
  })
}

for (const viewportWidth of [1280, 390]) {
  test(`animates Game choice toggles reversibly and respects reduced motion at ${viewportWidth}px`, async ({ page, request }) => {
    await createAndOpenBook(request, 'Game choice motion')
    const story = await createStartedStory(request, 'Choice motion')
    const turn = (await getStorySnapshot(request, story.id)).turns[0]
    const narrative = '车站的灯光照亮漫长的轨道，旅人沿着脚印继续探索。'.repeat(180)
    const edited = await request.patch(`/api/interactive/stories/${story.id}/turns/${turn.id}/narrative`, {
      data: { branch_id: 'main', expected_narrative: turn.narrative, narrative },
    })
    expect(edited.ok(), await edited.text()).toBe(true)
    const settings = await request.patch('/api/settings', { data: { layer: 'user', changes: { motion_intensity: 'system' } } })
    expect(settings.ok(), await settings.text()).toBe(true)
    await page.emulateMedia({ reducedMotion: 'no-preference' })
    await page.goto('/')
    await page.getByLabel('工作台侧边栏').getByRole('button', { name: '游戏', exact: true }).click()
    await page.setViewportSize({ width: viewportWidth, height: 900 })
    const toggle = page.locator('.nova-story-stage-composer [aria-controls][aria-expanded]')
    await expect(toggle).toBeEnabled()
    const timeline = page.locator('.nova-story-stage-content .nova-chat-canvas')
    // Composer controls can become ready before history is rendered and measured.
    await expect(timeline).toContainText(narrative)
    await expect(timeline).toHaveAttribute('aria-busy', 'false')
    await expect.poll(() => timeline.evaluate(el => el.scrollHeight - el.clientHeight)).toBeGreaterThan(600)
    // Wait for the route's entrance fade before sending input or measuring motion.
    await expect.poll(() => page.locator('.nova-story-choice-panel').evaluate(surface => {
      let opacity = 1
      for (let ancestor = surface.parentElement; ancestor; ancestor = ancestor.parentElement) {
        opacity *= Number(getComputedStyle(ancestor).opacity)
      }
      return opacity
    })).toBe(1)
    // Wheel over the timeline gutter so nested state panels do not consume it.
    await timeline.hover({ position: { x: 8, y: 80 } })
    await page.mouse.wheel(0, -600)
    await expect.poll(() => timeline.evaluate(el => el.scrollHeight - el.clientHeight - el.scrollTop)).toBeGreaterThan(400)
    const frames = await toggle.evaluate(async (element) => {
      const button = element as HTMLButtonElement
      const panel = document.getElementById(button.getAttribute('aria-controls')!)!
      const composer = button.closest('.nova-story-stage-composer')!
      const inputFloat = button.closest('.nova-story-input-float')!
      const surface = document.querySelector('.nova-story-choice-panel')!
      const timeline = document.querySelector('.nova-story-stage-content .nova-chat-canvas')!
      const samples: Array<{ opacity: number; backdropAncestorOpacity: number; bottom: number; floatHeight: number; scrollTop: number; scrollHeight: number; expanded: boolean; inert: boolean }> = []
      const sample = () => {
        let backdropAncestorOpacity = 1
        for (let ancestor = surface.parentElement; ancestor; ancestor = ancestor.parentElement) {
          backdropAncestorOpacity *= Number(getComputedStyle(ancestor).opacity)
        }
        samples.push({
          opacity: Number(getComputedStyle(surface).opacity) * backdropAncestorOpacity,
          backdropAncestorOpacity,
          bottom: composer.getBoundingClientRect().bottom,
          floatHeight: inputFloat.getBoundingClientRect().height,
          scrollTop: timeline.scrollTop,
          scrollHeight: timeline.scrollHeight,
          expanded: button.getAttribute('aria-expanded') === 'true',
          inert: panel.inert,
        })
      }
      sample()
      button.click()
      const start = performance.now()
      let reversed = false
      while (performance.now() - start < 400) {
        await new Promise(requestAnimationFrame)
        sample()
        if (!reversed && performance.now() - start >= 80) {
          button.click()
          reversed = true
        }
      }
      return samples
    })
    await test.info().attach('choice-motion-frames', { body: JSON.stringify(frames), contentType: 'application/json' })
    expect(frames[0].opacity).toBe(0)
    expect(frames.some(frame => frame.opacity > 0.1 && frame.expanded)).toBe(true)
    expect(frames.some(frame => frame.opacity > 0.1 && !frame.expanded && frame.inert)).toBe(true)
    expect(frames.at(-1)?.opacity).toBe(0)
    // Opacity on a blur ancestor cuts off the text backdrop until its final frame.
    expect(frames.every(frame => frame.backdropAncestorOpacity === 1)).toBe(true)
    for (const metric of ['floatHeight', 'scrollTop', 'scrollHeight'] as const) {
      expect(Math.max(...frames.map(frame => frame[metric])) - Math.min(...frames.map(frame => frame[metric])), metric).toBeLessThan(1)
    }
    expect(Math.max(...frames.map(frame => frame.bottom)) - Math.min(...frames.map(frame => frame.bottom))).toBeLessThan(1)
    expect(new Set(frames.filter(frame => frame.expanded).map(frame => frame.opacity)).size).toBeGreaterThan(2)
    await expect(page.getByRole('button', { name: '走进旧车站', exact: true })).toHaveCount(0)
    await toggle.press('Enter')
    await expect(page.getByRole('button', { name: '走进旧车站', exact: true })).toBeVisible()
    const panel = page.locator('.nova-story-choice-panel')
    await expect(panel).toHaveCSS('opacity', '1')
    expect(await panel.evaluate(element => {
      const scrollButton = document.querySelector('[data-nova-scroll-to-bottom]')!
      const buttonBox = scrollButton.getBoundingClientRect()
      const x = buttonBox.x + buttonBox.width / 2
      const y = buttonBox.y + buttonBox.height / 2
      const box = element.getBoundingClientRect()
      return x < box.left || x > box.right || y < box.top || y > box.bottom
        || element.contains(document.elementFromPoint(x, y))
    })).toBe(true)
    await page.screenshot({ path: test.info().outputPath(`game-choice-overlay-${viewportWidth}.png`) })
    await toggle.press('Enter')
    await expect(toggle).toBeFocused()
    await expect(toggle).toHaveAttribute('aria-expanded', 'false')

    await page.emulateMedia({ reducedMotion: 'reduce' })
    await page.reload()
    await expect(toggle).toBeEnabled()
    const reducedFrames = await toggle.evaluate(async (element) => {
      const panel = document.getElementById(element.getAttribute('aria-controls')!)!
      ;(element as HTMLButtonElement).click()
      const opacities: number[] = []
      for (let i = 0; i < 8; i++) {
        await new Promise(requestAnimationFrame)
        opacities.push(Number(getComputedStyle(panel).opacity))
      }
      return opacities
    })
    expect(reducedFrames.at(-1)).toBe(1)
    expect(new Set(reducedFrames.filter(opacity => opacity > 0))).toEqual(new Set([1]))
  })
}

test('submits, streams, and persists a complete Game turn', { tag: '@smoke' }, async ({ page, request }) => {
  await createAndOpenBook(request, 'Game E2E Book')
  const story = await createStartedStory(request, 'Game E2E Story')

  await page.goto('/')
  await page.getByLabel('工作台侧边栏').getByRole('button', { name: '游戏', exact: true }).click()
  const composer = page.getByPlaceholder(/你要做什么/)
  await expect(composer).toBeVisible()
  await submitAgentChatMessage(page, composer, '推开石门')

  await expect(page.getByText('石门缓缓开启，暖色灯光照亮了前方的旧车站。')).toBeVisible()
  await page.getByRole('button', { name: '获取行动选择' }).click()
  await expect(page.getByText('走进旧车站', { exact: true })).toBeVisible()
  await expect(page.getByText('留在门外观察', { exact: true })).toBeVisible()

  await expect.poll(async () => {
    return (await getStorySnapshot(request, story.id)).turns
  }).toContainEqual(expect.objectContaining({ user: '推开石门', narrative: expect.stringContaining('石门缓缓开启') }))

  await page.reload()
  await page.getByLabel('工作台侧边栏').getByRole('button', { name: '游戏', exact: true }).click()
  await expect(page.getByText('石门缓缓开启，暖色灯光照亮了前方的旧车站。')).toHaveCount(1)
  await page.getByRole('button', { name: '获取行动选择' }).click()
  await page.getByText('走进旧车站', { exact: true }).click()
  await expect(page.locator('[data-action="send"]').filter({ visible: true })).toBeEnabled()
  await composer.press('Enter')

  await expect.poll(async () => (await getStorySnapshot(request, story.id)).turns).toHaveLength(3)
  await expect.poll(async () => (await getStorySnapshot(request, story.id)).turns[2]?.user).toBe('走进旧车站')
})

test('creates and switches to a branch from a persisted Game turn', { tag: '@smoke' }, async ({ page, request }) => {
  await createAndOpenBook(request, 'Game Branch E2E Book')
  const story = await createStartedStory(request, 'Game Branch E2E Story')

  await page.goto('/')
  await page.getByLabel('工作台侧边栏').getByRole('button', { name: '游戏', exact: true }).click()
  const composer = page.getByPlaceholder(/你要做什么/)
  await submitAgentChatMessage(page, composer, '推开石门')
  await expect.poll(async () => (await getStorySnapshot(request, story.id)).turns).toHaveLength(2)

  await page.getByRole('button', { name: '从此处创建分支' }).last().click()
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel('剧情线名称').fill('E2E 支线')
  await dialog.getByRole('button', { name: '创建并切换', exact: true }).click()

  await expect.poll(async () => getStoryBranches(request, story.id)).toContainEqual(
    expect.objectContaining({ title: 'E2E 支线', current: true }),
  )
})

test('lets the Game Agent maintain a branch plan and keeps planning user-controllable', async ({ page, request }) => {
  await createAndOpenBook(request, 'Game Planning E2E Book')
  const story = await createStartedStory(request, 'Game Planning E2E Story', { planningMode: 'enabled' })

  await page.goto('/')
  await page.getByLabel('工作台侧边栏').getByRole('button', { name: '游戏', exact: true }).click()
  const composer = page.getByPlaceholder(/你要做什么/)
  await submitAgentChatMessage(page, composer, `查看站台地图 ${gameBranchPlanMarker}`)

  await expect(page.getByText('你在站台地图上发现一条通往钟楼的维护通道。', { exact: true })).toBeVisible()
  await expect.poll(async () => (await getStorySnapshot(request, story.id)).branch_plan?.markdown).toContain('保留玩家离开车站的自由')

  const branchPlan = page.locator('[data-slot="collapsible"]').filter({
    has: page.getByRole('button', { name: /当前分支规划/ }),
  })
  await branchPlan.getByRole('button', { name: /当前分支规划/ }).click()
  await expect(branchPlan.getByRole('heading', { name: '当前意图', exact: true })).toBeVisible()
  await expect(branchPlan.getByText(/保留玩家离开车站的自由/)).toBeVisible()

  await page.getByRole('tab', { name: '控制', exact: true }).click()
  const planningSwitch = page.getByRole('switch', { name: '游戏规划' })
  await expect(planningSwitch).toBeChecked()
  await planningSwitch.click()
  await expect(planningSwitch).not.toBeChecked()
  await page.getByRole('tab', { name: '总览', exact: true }).click()
  await expect(page.getByText('规划已关闭').first()).toBeVisible()

  await page.reload()
  await page.getByLabel('工作台侧边栏').getByRole('button', { name: '游戏', exact: true }).click()
  await page.getByRole('tab', { name: '控制', exact: true }).click()
  await expect(page.getByRole('switch', { name: '游戏规划' })).not.toBeChecked()
  await page.getByRole('tab', { name: '总览', exact: true }).click()
  await branchPlan.getByRole('button', { name: /当前分支规划/ }).click()
  await expect(branchPlan.getByText(/保留玩家离开车站的自由/)).toBeVisible()
})

test('preserves the settled turn after a failed regeneration and replaces it on retry', async ({ page, request }) => {
  await createAndOpenBook(request, 'Game Regeneration E2E Book')
  const story = await createStartedStory(request, 'Game Regeneration E2E Story')

  await page.goto('/')
  await page.getByLabel('工作台侧边栏').getByRole('button', { name: '游戏', exact: true }).click()
  const composer = page.getByPlaceholder(/你要做什么/)
  await submitAgentChatMessage(page, composer, '聆听旧车站的广播 E2E_GAME_REGENERATE_FAILURE')
  await expect(page.getByText('第一次生成的钟声从旧车站深处传来。', { exact: true })).toBeVisible()
  await expect.poll(async () => (await getStorySnapshot(request, story.id)).turns).toHaveLength(2)

  await page.getByRole('button', { name: '重新生成这一轮' }).last().click()
  await expect.poll(async () => (await getModelStatus(request)).game_regeneration_failure_requests, { timeout: 20_000 })
    .toBeGreaterThan(0)
  await expect(page.getByRole('alert')).toBeVisible({ timeout: 20_000 })
  await expect(composer).toBeEnabled()
  const failedSnapshot = await getStorySnapshot(request, story.id)
  expect(failedSnapshot.turns).toHaveLength(2)
  expect(failedSnapshot.turns[1]?.narrative).toContain('第一次生成的钟声')

  await allowGameRegeneration(request)
  await page.getByRole('button', { name: '重新生成这一轮' }).last().click()
  await expect(page.getByText('重试后，月台广播给出了全新的撤离路线。', { exact: true })).toBeVisible()
  await expect.poll(async () => (await getStorySnapshot(request, story.id)).turns).toEqual([
    expect.objectContaining({ narrative: expect.stringContaining(gameOpeningNarrative) }),
    expect.objectContaining({
      user: '聆听旧车站的广播 E2E_GAME_REGENERATE_FAILURE',
      narrative: expect.stringContaining('重试后，月台广播给出了全新的撤离路线'),
    }),
  ])
})

test('queues a Game Follow Up and steers the active turn through the real runtime', async ({ page, request }) => {
  await createAndOpenBook(request, 'Game Follow Up E2E Book')
  const story = await createStartedStory(request, 'Game Follow Up E2E Story')

  await page.goto('/')
  await page.getByLabel('工作台侧边栏').getByRole('button', { name: '游戏', exact: true }).click()
  const composer = page.getByPlaceholder(/你要做什么/)
  try {
    await submitAgentChatMessage(page, composer, `先观察石门，等待下一步。${gameFollowUpDelayMarker}`)
    await expect.poll(async () => (await getModelStatus(request)).delayed_waiting_by_marker[gameFollowUpDelayMarker] ?? 0)
      .toBe(1)

    const followUp = `改为跟随脚印进入车站。${gameFollowUpMarker}`
    await submitAgentChatMessage(page, composer, followUp)
    const queue = page.getByRole('region', { name: '排队中的指令' }).filter({ visible: true })
    await expect(queue).toContainText(gameFollowUpMarker)
    await queue.getByRole('button', { name: '立即转向', exact: true }).click()
    await releaseDelayedRequest(request, gameFollowUpDelayMarker)

    await expect(page.getByText(gameFollowUpNarrative, { exact: true })).toBeVisible()
    await expect.poll(async () => (await getModelStatus(request)).request_counts[gameFollowUpMarker] ?? 0).toBe(1)
    await expect.poll(async () => (await getStorySnapshot(request, story.id)).turns).toEqual([
      expect.objectContaining({ narrative: expect.stringContaining(gameOpeningNarrative) }),
      expect.objectContaining({
        // Same-turn native steering retains the accepted original player
        // input; the additional instruction lives in that turn's journal.
        user: process.env.DENOVA_TEST_CODEX_EXE ? `先观察石门，等待下一步。${gameFollowUpDelayMarker}` : followUp,
        narrative: expect.stringContaining(gameFollowUpNarrative),
      }),
    ])
  } finally {
    await releaseDelayedRequest(request, gameFollowUpDelayMarker)
  }
})
