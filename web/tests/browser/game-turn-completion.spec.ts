import { expect, test, type Page } from '../support/fixtures'
import { createAndOpenBook, createStartedStory } from '../support/api'

const runId = 'game-completion-run'
const narrative = Array.from({ length: 24 }, (_, index) => `第 ${index + 1} 段。旅人沿着石阶走向城门，灯光照亮了墙上的旧地图。`).join('\n\n')

for (const { theme, width, motion } of [
  { theme: 'dark', width: 1440, motion: 'full' },
  { theme: 'light', width: 1440, motion: 'off' },
  { theme: 'dark', width: 390, motion: 'off' },
  { theme: 'light', width: 390, motion: 'full' },
  { theme: 'dark', width: 1440, motion: 'system' },
] as const) {
  test(`Game completes in place with ${theme} theme at ${width}px and ${motion} motion`, async ({ page, request }) => {
    test.setTimeout(60_000)
    await createAndOpenBook(request, `Game completion ${theme} ${width}`)
    const story = await createStartedStory(request, 'Stable completion')
    const snapshotURL = `/api/interactive/stories/${story.id}/snapshot?branch=main`
    let snapshot = await (await request.get(snapshotURL)).json()
    const settings = await (await request.get('/api/settings')).json()
    await page.route(/\/api\/(?:projects\/[^/]+\/)?settings$/, route => route.fulfill({
      json: { ...settings, effective: { ...settings.effective, theme, motion_intensity: motion } },
    }))
    await page.route('**/api/**/snapshot*', route => route.fulfill({ json: snapshot }))
    await page.route('**/api/interactive/stories/*/images/generate', route => route.fulfill({ json: { skipped: true } }))
    await page.emulateMedia({ reducedMotion: motion === 'system' ? 'reduce' : 'no-preference' })
    await page.addInitScript(() => {
      const fetch = window.fetch.bind(window)
      window.fetch = async (...args) => {
        const response = await fetch(...args)
        if (!response.headers.has('x-test-game-completion')) return response
        const bytes = new Uint8Array(await response.arrayBuffer())
        return new Response(new ReadableStream({ start(controller) {
          controller.enqueue(bytes)
          window.addEventListener('test-game-completion', event => {
            const data = (event as CustomEvent<string | null>).detail
            if (data === null) controller.close()
            else controller.enqueue(new TextEncoder().encode(data))
          })
        } }), { headers: { 'content-type': 'text/event-stream' } })
      }
    })
    await page.route('**/api/interactive/chat', route => route.fulfill({
      contentType: 'text/event-stream', headers: { 'x-test-game-completion': 'true' },
      body: event('agent_cycle_started', { id: 'game-completion-output', command_id: 'game-completion-command', operation_id: runId, run_id: runId, cycle: 1, delivery: 'start_turn', message: '走向城门' })
        + event('thinking', { content: 'Check the scene.', run_id: runId, display_segment_id: 'scene-thinking' })
        + event('chunk', { content: narrative, run_id: runId, agent_kind: 'interactive_story', display_phase: 'candidate', display_segment_id: 'scene-narrative' }),
    }))
    await page.setViewportSize({ width: 1440, height: 900 })
    await page.goto('/')
    await page.getByLabel('工作台侧边栏').getByRole('button', { name: '游戏', exact: true }).click()
    await page.setViewportSize({ width, height: 900 })
    // The initial shell can precede the selected story's hydrated stage.
    // Bind the journey to that stage before editing its conversation draft.
    await expect(page.getByTestId('story-stage-card').getByText(snapshot.current_turn.narrative, { exact: true })).toBeVisible()
    const input = page.getByPlaceholder(/你要做什么/)
    await input.fill('走向城门')
    await expect(input).toHaveText('走向城门')
    await page.locator('[data-action="send"]').filter({ visible: true }).click()
    const prose = page.locator('.nova-streamdown').filter({ hasText: '第 24 段。' })
    await expect(prose).toBeVisible()
    const proseNode = await prose.elementHandle()
    const row = page.locator('[data-nova-chat-row-key]').filter({ has: prose })
    // Game narrative is already the visible reply while it streams. Committing
    // it must not move it out of an execution disclosure or add a new wrapper.
    await expect(row).not.toHaveAttribute('data-agent-execution-content', /.+/)
    const ledger = page.getByRole('region', { name: '当前状态', exact: true })
    await expect(ledger).toHaveCount(0)
    await emit(page, event('tool_call', { id: 'submit-turn', name: 'submit_interactive_turn', args: '{}', run_id: runId, display_segment_id: 'submit-turn' }))
    await emit(page, event('tool_result', { id: 'submit-turn', name: 'submit_interactive_turn', content: 'Turn submitted.', run_id: runId, display_segment_id: 'submit-turn' }))
    await expect(ledger).toHaveCount(0)
    const readingEarlier = theme === 'light' && width === 1440
    if (readingEarlier) {
      await prose.locator('p').last().hover()
      await page.mouse.wheel(0, -250)
      await expect(page.getByRole('button', { name: '回到底部', exact: true })).toBeVisible()
    }
    const beforeCommit = await prose.boundingBox()
    const turn = {
      id: 'confirmed-turn', parent_id: snapshot.current_turn.id, branch_id: 'main', ts: new Date().toISOString(),
      user: '走向城门', narrative, run_id: runId, agent_kind: 'interactive_story', state_status: 'applied',
      display_events: [
        { id: 'scene-thinking', role: 'thinking', content: 'Check the scene.', run_id: runId, status: 'success' },
        { id: 'narrative-anchor', role: 'narrative' },
        { id: 'submit-turn', role: 'tool_call', name: 'submit_interactive_turn', args: '{}', result: 'Turn submitted.', run_id: runId, status: 'success' },
      ],
    }
    snapshot = { ...snapshot, turns: [...snapshot.turns, turn], current_turn: turn, turn_count: 2 }
    const entranceFrames = await sampleEntrance(page, event('interactive_turn_persisted', {
      story_id: story.id, branch_id: 'main', turn_count: 2, state: snapshot.state, turn,
    }))
    await test.info().attach('state-entrance-frames', { body: JSON.stringify(entranceFrames), contentType: 'application/json' })
    if (motion === 'off') {
      expect(entranceFrames.every(frame => frame.opacity === 1 && frame.y === 0)).toBe(true)
    } else {
      expect(entranceFrames.filter(frame => frame.opacity > 0 && frame.opacity < 1).length).toBeGreaterThan(2)
      expect(entranceFrames.at(-1)!.time - entranceFrames[0].time).toBeGreaterThanOrEqual(motion === 'system' ? 120 : 240)
      if (motion === 'system') expect(entranceFrames.every(frame => frame.y === 0)).toBe(true)
      else expect(entranceFrames.some(frame => frame.y > 4)).toBe(true)
    }
    // State is available as soon as the commit is confirmed, even while an
    // external runtime is still finishing its transport.
    await expect(ledger).toBeVisible()
    await expect(ledger).toHaveCSS('opacity', '1')
    await expect(page.locator('[data-action="stop"]').filter({ visible: true })).toBeVisible()
    expect(await proseNode!.evaluate(node => node.isConnected)).toBe(true)
    if (!readingEarlier) {
      await expect.poll(async () => {
        const heading = (await ledger.locator('[data-nova-chat-after-content-reveal]').boundingBox())!
        const composer = (await page.getByPlaceholder(/你要做什么/).boundingBox())!
        return heading.y + heading.height <= composer.y
      }).toBe(true)
      // Wait for the small native smooth scroll before testing transport close.
      await expect.poll(() => prose.evaluate(async node => {
        const start = node.getBoundingClientRect().y
        await new Promise<void>(resolve => requestAnimationFrame(() => requestAnimationFrame(() => resolve())))
        return Math.abs(node.getBoundingClientRect().y - start)
      })).toBeLessThanOrEqual(0.1)
    }
    // Reveal the heading and its surrounding spacing, never the full ledger.
    expect(Math.abs((await prose.boundingBox())!.y - beforeCommit!.y)).toBeLessThanOrEqual(readingEarlier ? 2 : 96)
    const beforeDone = await prose.boundingBox()
    const ledgerNode = await ledger.elementHandle()
    await emit(page, event('done', {}))
    await emit(page, null)
    await expect(page.locator('[data-action="stop"]').filter({ visible: true })).toHaveCount(0)
    expect(await proseNode!.evaluate(node => node.isConnected)).toBe(true)
    expect(await ledgerNode!.evaluate(node => node.isConnected)).toBe(true)
    expect(Math.abs((await prose.boundingBox())!.y - beforeDone!.y)).toBeLessThanOrEqual(2)
    expect(await ledger.evaluate(node => node.scrollWidth <= node.clientWidth)).toBe(true)
    await page.screenshot({ path: test.info().outputPath(`game-completion-${theme}-${width}.png`) })
  })
}

function event(type: string, data: Record<string, unknown>) {
  return `event: ${type}\ndata: ${JSON.stringify(data)}\n\n`
}

async function emit(page: Page, detail: string | null) {
  await page.evaluate(detail => window.dispatchEvent(new CustomEvent('test-game-completion', { detail })), detail)
}

// Sample rendered frames, not just the final opacity, so an instant mount or an
// imperceptibly short fade cannot pass as a smooth completion transition.
async function sampleEntrance(page: Page, detail: string) {
  return page.evaluate(detail => new Promise<{ time: number; opacity: number; y: number }[]>(resolve => {
    const frames: { time: number; opacity: number; y: number }[] = []
    const started = performance.now()
    const sample = (time: number) => {
      const panel = document.querySelector('.story-state-ledger')
      if (panel) {
        const style = getComputedStyle(panel)
        const opacity = Number(style.opacity)
        frames.push({ time, opacity, y: new DOMMatrixReadOnly(style.transform).m42 })
        if (opacity === 1) return resolve(frames)
      }
      if (time - started > 1500) return resolve(frames)
      requestAnimationFrame(sample)
    }
    requestAnimationFrame(sample)
    window.dispatchEvent(new CustomEvent('test-game-completion', { detail }))
  }), detail)
}
