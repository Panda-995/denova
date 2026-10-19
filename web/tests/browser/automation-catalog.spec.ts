import { randomUUID } from 'node:crypto'
import { expect, test } from '../support/fixtures'
import { createAndOpenBook } from '../support/api'

test('loads a large automation catalog without per-Project request fanout', async ({ page, request }) => {
  test.setTimeout(90_000)
  const book = await createAndOpenBook(request, 'Automation catalog')
  const projectIDs = [book.projectId]
  for (let index = 1; index < 50; index++) {
    const response = await request.post('/api/books/create', { data: {
      title: `Catalog ${randomUUID()}`, author: 'Denova E2E',
    } })
    expect(response.ok(), await response.text()).toBe(true)
    projectIDs.push((await response.json()).project_id)
  }
  const names = ['First Project task', 'Last Project task']
  for (const [index, projectID] of [projectIDs[0], projectIDs.at(-1)!].entries()) {
    const response = await request.post('/api/automations', { data: {
      scope: 'workspace', target: { kind: 'workspace', project_id: projectID },
      enabled: false, name: names[index], template: 'custom_prompt', prompt: 'Review the current work.',
    } })
    expect(response.ok(), await response.text()).toBe(true)
  }
  const reads: string[] = []
  page.on('request', request => {
    const path = new URL(request.url()).pathname
    if (request.method() === 'GET' && path.startsWith('/api/automations')) reads.push(path)
  })
  await page.goto('/')
  const catalog = page.waitForResponse(response => new URL(response.url()).pathname === '/api/automations/catalog')
  await page.getByLabel('工作台侧边栏').getByRole('button', { name: '自动化', exact: true }).click()
  const groups = (await (await catalog).json()).projects as Array<{ project_id: string; tasks: Array<{ name: string; target: { project_id: string } }> }>
  expect(groups.filter(group => projectIDs.includes(group.project_id))).toHaveLength(50)
  for (const name of names) await expect(page.getByRole('button', { name: new RegExp(name) })).toBeVisible()
  await expect(page.locator('[data-slot=loading-state]:visible')).toHaveCount(0)
  expect(reads.filter(path => path === '/api/automations' || path === '/api/automations/inbox')).toEqual([])
  // Development StrictMode may replay the mount; request count stays constant.
  expect(reads.filter(path => path === '/api/automations/catalog').length).toBeLessThanOrEqual(2)
  await page.getByLabel('工作台侧边栏').getByRole('button', { name: '设置', exact: true }).click()
  await expect(page.locator('[data-nova-settings-content]')).toBeVisible()
})
