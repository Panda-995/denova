import { expect, test } from '../support/fixtures'
import { createAndOpenBook, createStartedStory, getStorySnapshot } from '../support/api'

test('loads the selected story alongside resources and reveals it before the branch list', async ({ page, request }) => {
  await createAndOpenBook(request, 'Game parallel loading')
  const story = await createStartedStory(request, 'Resumed story')
  const turn = (await getStorySnapshot(request, story.id)).turns[0]
  const created = await request.post(`/api/interactive/stories/${story.id}/branches`, {
    data: { parent_event_id: turn.id, title: 'Resumed branch' },
  })
  expect(created.ok(), await created.text()).toBe(true)
  const branch = await created.json()
  await page.goto('/')
  await expect(page.getByLabel('工作台侧边栏')).toBeVisible()
  await expect(page.locator('[data-slot=loading-state]:visible')).toHaveCount(0)

  let releaseResources!: () => void
  let releaseBranches!: () => void
  const resources = new Promise<void>(resolve => { releaseResources = resolve })
  const branches = new Promise<void>(resolve => { releaseBranches = resolve })
  let snapshotRequests = 0
  page.on('request', request => {
    if (new URL(request.url()).pathname === `/api/interactive/stories/${story.id}/snapshot`) snapshotRequests++
  })
  await page.route(/\/api\/(interactive\/tellers|game-planning-templates)$/, async route => {
    await resources
    await route.continue()
  })
  await page.route(`**/api/interactive/stories/${story.id}/branches`, async route => {
    await branches
    await route.continue()
  })
  try {
    const snapshotReady = page.waitForResponse(response => new URL(response.url()).pathname === `/api/interactive/stories/${story.id}/snapshot`)
    await page.getByLabel('工作台侧边栏').getByRole('button', { name: '游戏', exact: true }).click()
    await expect.poll(() => snapshotRequests, { timeout: 5000 }).toBe(1)
    expect((await (await snapshotReady).json()).branch_id).toBe(branch.id)
    releaseResources()
    await expect(page.getByText(turn.narrative, { exact: true })).toBeVisible({ timeout: 5000 })
    const branchesReady = page.waitForResponse(response => new URL(response.url()).pathname === `/api/interactive/stories/${story.id}/branches`)
    releaseBranches()
    await branchesReady
    await page.getByRole('tab', { name: '路线', exact: true }).click()
    await expect(page.getByText('Resumed branch', { exact: true }).first()).toBeVisible()
    expect(snapshotRequests).toBe(1)
    await page.getByLabel('工作台侧边栏').getByRole('button', { name: '写作', exact: true }).click()
    await page.getByLabel('工作台侧边栏').getByRole('button', { name: '游戏', exact: true }).click()
    await expect.poll(() => snapshotRequests).toBe(2)
    await expect(page.getByText(turn.narrative, { exact: true })).toBeVisible()
  } finally {
    releaseResources()
    releaseBranches()
  }
})
