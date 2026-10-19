import { expect, test } from '../support/fixtures'
import { createAndOpenBook, createStartedStory } from '../support/api'
import type { InteractiveSnapshotResponse } from '../../src/features/interactive/types'

test('loads Game history progressively and fetches execution only when expanded', async ({ page, request }) => {
  test.setTimeout(180_000)
  await createAndOpenBook(request, 'Progressive Game history')
  const story = await createStartedStory(request, 'History window')
  const snapshotURL = `/api/interactive/stories/${story.id}/snapshot?branch=main`
  const readSnapshot = async () => (await (await request.get(snapshotURL)).json()) as InteractiveSnapshotResponse
  // Use the actual command/commit path, including tool evidence and current state.
  for (let index = 1; index < 13; index += 1) {
    const response = await request.post('/api/interactive/chat', { data: {
      command_id: `history-${story.id}-${index}`, mode: 'story', story_id: story.id, branch: 'main', message: `行动 ${index}`,
    } })
    expect(response.ok(), await response.text()).toBe(true)
    await expect.poll(async () => (await readSnapshot()).turn_count).toBe(index + 1)
  }
  const initial = await readSnapshot()
  expect(initial.turns).toHaveLength(10)
  expect(initial.turn_start).toBe(3)
  expect(initial.has_earlier_turns).toBe(true)
  expect(initial.current_turn?.id).toBe(initial.turns.at(-1)?.id)
  expect(initial.turns.some(turn => Boolean(turn.execution_cursor))).toBe(true)
  expect(JSON.stringify(initial)).not.toContain('model_context_messages')

  let detailsRequested = 0
  let releaseDetails: (() => void) | undefined
  const detailsGate = new Promise<void>(resolve => { releaseDetails = resolve })
  await page.route(`**/stories/${story.id}/history/execution?**`, async route => {
    detailsRequested += 1
    const response = await route.fetch()
    await detailsGate
    await route.fulfill({ response })
  })
  let historyRequested = 0
  let releaseHistory: (() => void) | undefined
  const historyGate = new Promise<void>(resolve => { releaseHistory = resolve })
  await page.route(`**/stories/${story.id}/history?**`, async route => {
    historyRequested += 1
    const response = await route.fetch()
    await historyGate
    await route.fulfill({ response })
  })
  try {
    await page.setViewportSize({ width: 2200, height: 1000 })
    await page.goto('/')
    await page.getByLabel('工作台侧边栏').getByRole('button', { name: '游戏', exact: true }).click()
    const timeline = page.locator('.nova-story-stage-content .nova-chat-canvas')
    await expect(timeline).toHaveAttribute('aria-busy', 'false')
    await expect(page.getByPlaceholder(/你要做什么/)).toBeVisible()
    await expect(timeline.getByRole('region', { name: '故事开局', exact: true })).toHaveCount(0)
    await expect(timeline.getByText('行动 12', { exact: true })).toBeVisible()
    await expect(timeline.getByRole('region', { name: '当前状态', exact: true })).toBeVisible()
    expect(detailsRequested).toBe(0)
    expect(historyRequested).toBe(0)
    await expect(page.getByRole('button', { name: '跳转到第 13 轮', exact: true })).toHaveCount(1)
    await expect(page.getByRole('button', { name: '跳转到第 1 轮', exact: true })).toHaveCount(1)

    const process = timeline.locator('[data-agent-execution-process] button').last()
    await process.click()
    await expect.poll(() => detailsRequested).toBe(1)
    await expect(process).toHaveAttribute('aria-busy', 'true')
    releaseDetails!()
    await expect(process).toHaveAttribute('aria-expanded', 'true')
    await expect(timeline.locator('[data-agent-execution-content]').first()).toBeVisible()
    await process.click()
    await process.click()
    expect(detailsRequested).toBe(1)
    await process.click()

    // Hold the older page in flight and compare the same visible row before
    // and after prepend. Loading must preserve the reader's pixel position.
    await timeline.hover()
    await page.mouse.wheel(0, -100_000)
    await expect.poll(() => historyRequested).toBe(1)
    await expect.poll(() => timeline.evaluate(element => element.scrollTop)).toBe(0)
    await expect(timeline.locator('[data-nova-chat-row-key]').first()).toHaveCSS('transform', 'none')
    const anchor = await timeline.evaluate(element => {
      const top = element.getBoundingClientRect().top
      const row = [...element.querySelectorAll<HTMLElement>('[data-nova-chat-row-key]')].find(row => row.getBoundingClientRect().bottom > top)!
      return { key: row.dataset.novaChatRowKey!, top: row.getBoundingClientRect().top }
    })
    releaseHistory!()
    await expect(timeline.getByRole('button', { name: '正在加载更早消息…', exact: true })).toHaveCount(0)
    const retained = timeline.locator(`[data-nova-chat-row-key="${anchor.key}"]`)
    await expect.poll(async () => Math.abs((await retained.boundingBox())!.y - anchor.top)).toBeLessThan(3)
    await timeline.evaluate(element => { element.scrollTop = 0 })
    await expect(timeline.getByRole('region', { name: '故事开局', exact: true })).toBeInViewport()
    await expect(timeline.getByText('暮色落在旧车站外，石门后的轨道传来遥远的回声。', { exact: true })).toBeInViewport()
    expect(historyRequested).toBe(1)

    await page.screenshot({ path: test.info().outputPath('game-history-dark-wide.png'), animations: 'disabled' })
    await page.reload()
    await expect(timeline).toHaveAttribute('aria-busy', 'false')
    await page.getByLabel('剧情轮次导航', { exact: true }).hover()
    await page.getByRole('button', { name: '跳转到第 1 轮', exact: true }).click()
    await expect(timeline.getByText('暮色落在旧车站外，石门后的轨道传来遥远的回声。', { exact: true })).toBeInViewport()
    expect(historyRequested).toBe(2)
    const settings = await (await request.get('/api/settings')).json()
    const changed = await request.patch('/api/settings', { data: { layer: 'user', base_revision: settings.revisions.user, changes: { theme: 'light' } } })
    expect(changed.ok(), await changed.text()).toBe(true)
    await page.setViewportSize({ width: 390, height: 844 })
    await page.reload()
    await expect(page.getByPlaceholder(/你要做什么/)).toBeVisible()
    await expect(timeline).toHaveAttribute('aria-busy', 'false')
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
    await page.getByRole('button', { name: '选择故事线', exact: true }).click()
    await page.getByRole('button', { name: '剧情轮次导航', exact: true }).click()
    const history = page.getByRole('dialog', { name: '剧情轮次导航', exact: true })
    await expect(history.getByRole('button', { name: '跳转到第 1 轮', exact: true })).toBeVisible()
    await page.screenshot({ path: test.info().outputPath('game-history-light-narrow.png'), animations: 'disabled' })
    await history.getByRole('button', { name: '跳转到第 1 轮', exact: true }).click()
    await expect(history).toBeHidden()
    await expect(timeline.getByText('暮色落在旧车站外，石门后的轨道传来遥远的回声。', { exact: true })).toBeInViewport()
    expect(historyRequested).toBe(3)
    const restored = await readSnapshot()
    expect(restored.turn_count).toBe(13)
    expect(restored.state).toEqual(initial.state)
    expect(restored.current_turn).toEqual(initial.current_turn)
  } finally {
    releaseDetails?.()
    releaseHistory?.()
  }
})
