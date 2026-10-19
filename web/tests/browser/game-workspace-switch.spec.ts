import { expect, test } from '../support/fixtures'
import { createAndOpenBook, createStartedStory } from '../support/api'

test('switches Game projects without binding the previous story to the new project', async ({ page, request }) => {
  const firstBook = await createAndOpenBook(request, 'Game switch first')
  const firstStory = await createStartedStory(request, 'First project story')
  const secondBook = await createAndOpenBook(request, 'Game switch second')
  const secondStory = await createStartedStory(request, 'Second project story')
  const bindings: { projectId: string; storyId: string }[] = []
  page.on('request', request => {
    const url = new URL(request.url())
    const projectId = url.pathname.match(/^\/api\/projects\/([^/]+)\/conversation-config$/)?.[1]
    const storyId = url.searchParams.get('story_id')
    if (projectId && storyId) bindings.push({ projectId, storyId })
  })
  await page.goto('/')
  await page.getByLabel('工作台侧边栏').getByRole('button', { name: '游戏', exact: true }).click()
  await expect.poll(() => bindings).toContainEqual({ projectId: secondBook.projectId, storyId: secondStory.id })

  let releaseIndex!: () => void
  const indexGate = new Promise<void>(resolve => { releaseIndex = resolve })
  await page.route(/\/api\/interactive\/stories$/, async route => {
    await indexGate
    await route.continue()
  })
  try {
    await page.getByRole('button', { name: `切换书籍，当前：${secondBook.title}`, exact: true }).click()
    await page.getByRole('menuitem').filter({ hasText: firstBook.title }).click()
    await expect(page.getByRole('button', { name: `切换书籍，当前：${firstBook.title}`, exact: true })).toBeVisible()
    await expect(page.getByTestId('story-stage')).toHaveCount(0)
  } finally {
    releaseIndex()
  }
  await expect.poll(() => bindings).toContainEqual({ projectId: firstBook.projectId, storyId: firstStory.id })
  expect(bindings).not.toContainEqual({ projectId: firstBook.projectId, storyId: secondStory.id })
  expect(bindings).not.toContainEqual({ projectId: secondBook.projectId, storyId: firstStory.id })
  await expect(page.getByPlaceholder(/你要做什么/)).toBeVisible()
})
