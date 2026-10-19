import { mkdtemp } from 'node:fs/promises'
import path from 'node:path'
import { runtimeRoot } from '../../scripts/e2e-paths.mjs'
import { expect, test } from '../support/fixtures'
import { createAndOpenBook, createAgentChatSession, registerAgentChatProject } from '../support/api'
import { openAgentChatSession, openAgentChatWorkbench, openWritingAgent, submitAgentChatMessage } from '../support/agent-chat'
import { getModelStatus, releaseDelayedRequest } from '../support/model'

for (const product of ['writing', 'general'] as const) {
  test(`${product} completes consecutive turns without reloading the observed history`, async ({ page, request }) => {
    const book = await createAndOpenBook(request, `Settlement ${product}`)
    const project = product === 'general'
      ? await registerAgentChatProject(request, await mkdtemp(path.join(runtimeRoot, 'settlement-project-')))
      : undefined
    const session = project ? await createAgentChatSession(request, project.id, 'Settlement session') : undefined
    const apiPath = `/api/projects/${project?.id ?? book.projectId}/agent-chat`
    await page.goto('/')
    if (project && session) await openAgentChatWorkbench(page)
    const composer = project && session
      ? await openAgentChatSession(page, project.id, session.title)
      : await openWritingAgent(page)
    await expect(page.locator('[data-action="stop"]').filter({ visible: true })).toHaveCount(0)
    const historyReads: string[] = []
    page.on('request', value => {
      if (new URL(value.url()).pathname === `${apiPath}/session/messages`) historyReads.push(value.url())
    })

    for (let turn = 1; turn <= 2; turn++) {
      await submitAgentChatMessage(page, composer, `Turn ${turn}. E2E_DELAYED_AGENT_REPLY`)
      await expect.poll(async () => (await getModelStatus(request)).delayed_waiting_by_marker.E2E_DELAYED_AGENT_REPLY ?? 0).toBe(1)
      const settled = page.waitForResponse(async response => {
        if (new URL(response.url()).pathname !== `${apiPath}/chat/active` || !response.ok()) return false
        const projection = await response.json()
        return projection.active === false && projection.phase === 'idle'
      })
      await releaseDelayedRequest(request, 'E2E_DELAYED_AGENT_REPLY')
      await settled
      await expect(page.locator('[data-action="stop"]').filter({ visible: true })).toHaveCount(0)
      await expect(page.getByText('Recovered response completed exactly once.', { exact: true })).toHaveCount(turn)
      expect(historyReads).toEqual([])
    }
    await page.screenshot({ path: test.info().outputPath(`${product}-settled.png`) })
    expect(historyReads).toEqual([])
  })
}
