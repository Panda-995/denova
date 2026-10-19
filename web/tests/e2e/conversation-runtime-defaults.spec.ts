import { mkdtemp } from 'node:fs/promises'
import path from 'node:path'
import { runtimeRoot } from '../../scripts/e2e-paths.mjs'
import { expect, test } from '../support/fixtures'
import { createAndOpenBook, createStory, registerAgentChatProject } from '../support/api'
import { openAgentChatSession, openAgentChatWorkbench, openWritingAgent, submitAgentChatMessage } from '../support/agent-chat'

// Persist Codex selections through the real settings/session APIs, then exercise
// the real composer PATCH. External discovery is isolated from host CLI accounts.
for (const mode of ['Writing', 'General'] as const) {
  for (const custom of [false, true]) {
    test(`${mode} remembers a runtime switch for new ${custom ? 'custom Agent' : 'built-in Agent'} conversations`, async ({ page, request, browserDiagnostics }) => {
      await page.route('**/api/agent-runtimes', route => route.fulfill({ json: { items: [
        { id: 'native', status: 'ready' }, { id: 'codex', status: 'not_installed' }, { id: 'claude', status: 'not_installed' },
      ] } }))
      await page.route('**/api/agent-runtimes/*/models', route => route.fulfill({ json: { items: [] } }))
      const writing = mode === 'Writing'
      const agentKind = writing ? 'ide' : 'general'
      const preferences = { selected: 'codex', codex: { model: 'saved-codex-model', effort: 'medium' }, claude: { model: 'saved-claude-model' } }
      const customID = `runtime-default-${agentKind}`
      const seeded = await request.patch('/api/settings', { data: { layer: 'user', changes: {
        theme: writing ? 'dark' : 'light',
        agent_runtimes: { [agentKind]: preferences },
        ...(custom ? { custom_agents: [{ id: customID, name: 'Runtime default Agent', enabled: true,
          contract: writing ? 'writing.primary.v1' : 'project.general.v1', instructions: 'Keep the saved role.', runtime: preferences }] } : {}),
      } } })
      expect(seeded.ok(), await seeded.text()).toBe(true)
      const project = writing ? await createAndOpenBook(request, 'Runtime defaults')
        : { projectId: (await registerAgentChatProject(request, await mkdtemp(path.join(runtimeRoot, 'runtime-default-')))).id }
      const base = `/api/projects/${project.projectId}`
      const createSession = async (title: string) => {
        const response = await request.post(`${base}/agent-chat/sessions`, { data: { title, ...(custom ? { custom_agent_id: customID } : {}) } })
        expect(response.ok(), await response.text()).toBe(true)
        return response.json() as Promise<{ id: string; title: string }>
      }
      const readConfig = async (sessionID: string) => {
        const response = await request.get(`${base}/conversation-config?mode=agent_chat&session_id=${sessionID}`)
        expect(response.ok(), await response.text()).toBe(true)
        return response.json()
      }
      const first = await createSession('Switch this conversation')
      const other = await createSession('Keep Codex here')
      const originalOther = await readConfig(other.id)
      expect((await readConfig(first.id)).runtime.kind).toBe('codex')
      await page.goto('/')
      let composer = writing ? await openWritingAgent(page) : (await openAgentChatWorkbench(page), await openAgentChatSession(page, project.projectId, first.title))
      if (writing) {
        await page.getByRole('button', { name: '会话历史', exact: true }).filter({ visible: true }).click()
        await page.getByRole('option').filter({ hasText: first.title }).click()
      }
      const trigger = page.locator('[data-model-profile-trigger]').filter({ visible: true })
      await expect(trigger).toBeEnabled()
      await trigger.click()
      await page.getByRole('menuitem', { name: '运行时：Codex', exact: true }).click()
      const retryRememberedSelection = writing && !custom
      if (retryRememberedSelection) {
        browserDiagnostics.allow(/^http\.5xx: PATCH .*\/conversation-config returned 500$/)
        browserDiagnostics.allow(/^console\.error: Failed to load resource:.*500.*\/conversation-config/)
        // A saved conversation can survive a failed defaults response. Keep the
        // real commit and reload, and simulate only that typed failure response.
        await page.route(`**${base}/conversation-config`, async route => {
          const committed = await route.fetch()
          expect(committed.ok(), await committed.text()).toBe(true)
          await route.fulfill({ status: 500, json: { code: 'api.conversationConfig.rememberSelectionFailed', error: 'Could not remember the runtime selection.' } })
        }, { times: 1 })
      }
      const switchResponse = page.waitForResponse(response => response.request().method() === 'PATCH' && new URL(response.url()).pathname === `${base}/conversation-config`)
      await page.getByRole('menuitem', { name: 'Native', exact: true }).click()
      expect((await switchResponse).request().postDataJSON().binding.session_id).toBe(first.id)
      if (retryRememberedSelection) {
        // Wait for the switched engine before reopening its error submenu.
        // Hover keeps an already open submenu open across the engine change.
        await page.getByRole('menuitem', { name: '运行时：Native', exact: true }).hover()
        const saveError = page.getByRole('alert').filter({ hasText: 'Could not remember the runtime selection.' })
        await expect(saveError).toBeVisible()
        await expect(page.getByRole('menuitem', { name: 'Native', exact: true })).toBeEnabled()
        await page.getByRole('menuitem', { name: 'Native', exact: true }).click()
        await expect(saveError).toHaveCount(0)
      }
      await expect(page.getByRole('menuitem', { name: '运行时：Native', exact: true })).toBeVisible()
      await page.keyboard.press('Escape')
      await expect(page.getByRole('menu')).toHaveCount(0)
      const saved = await (await request.get('/api/settings')).json()
      const remembered = custom ? saved.user.custom_agents.find((agent: { id: string }) => agent.id === customID).runtime : saved.user.agent_runtimes[agentKind]
      expect(remembered).toEqual({ ...preferences, selected: 'native' })
      if (custom) expect(saved.user.agent_runtimes[agentKind]).toEqual(preferences)
      expect(await readConfig(other.id)).toEqual(originalOther)
      // Revisiting an older Codex session must not overwrite the new default.
      await composer.fill('/new')
      await page.locator('[data-action="send"]').filter({ visible: true }).click()
      composer = page.getByPlaceholder(/输入消息/).filter({ visible: true })
      const freshRequest = page.waitForRequest(candidate => candidate.method() === 'POST' && new URL(candidate.url()).pathname === `${base}/agent-chat/chat`)
      await submitAgentChatMessage(page, composer, 'Verify the new Native conversation.')
      const freshID = (await freshRequest).postDataJSON().session_id as string
      expect(freshID).not.toBe(first.id)
      await expect(page.getByText('Deterministic E2E response completed.', { exact: true }).filter({ visible: true })).toBeVisible()
      const fresh = await readConfig(freshID)
      expect(fresh.runtime.kind).toBe('native')
      expect(fresh.custom_agent_id || '').toBe(custom ? customID : '')
      await page.reload()
      if (writing) await openWritingAgent(page)
      else { await openAgentChatWorkbench(page); await openAgentChatSession(page, project.projectId, 'Keep Codex here') }
      expect((await readConfig(freshID)).runtime.kind).toBe('native')
      expect(await readConfig(other.id)).toEqual(originalOther)
      await page.screenshot({ path: test.info().outputPath(`${agentKind}-${custom ? 'custom' : 'builtin'}-runtime-default.png`) })
    })
  }
}

test('Game remembers the runtime default and new stories ignore older story activity', async ({ request }) => {
  const seeded = await request.patch('/api/settings', { data: { layer: 'user', changes: {
    agent_runtimes: { interactive_story: { selected: 'codex', codex: { model: 'saved-game-model' } } },
  } } })
  expect(seeded.ok(), await seeded.text()).toBe(true)
  const book = await createAndOpenBook(request, 'Game runtime defaults')
  const first = await createStory(request, 'Switch this story')
  const other = await createStory(request, 'Keep Codex on this story')
  const configPath = `/api/projects/${book.projectId}/conversation-config`
  const readConfig = async (storyID: string) => (await request.get(`${configPath}?mode=interactive&story_id=${storyID}&branch_id=main`)).json()
  const initial = await readConfig(first.id)
  const originalOther = await readConfig(other.id)
  expect(initial.runtime.kind).toBe('codex')
  const changed = await request.patch(configPath, { data: { binding: { mode: 'interactive', story_id: first.id, branch_id: 'main' },
    base_revision: initial.revision, changes: { runtime: { kind: 'native' } },
  } })
  expect(changed.ok(), await changed.text()).toBe(true)
  const saved = await (await request.get('/api/settings')).json()
  expect(saved.user.agent_runtimes.interactive_story).toEqual({ selected: 'native', codex: { model: 'saved-game-model' } })
  // Updating an older story makes its unchanged Codex snapshot the most recent.
  const touched = await request.patch(`/api/interactive/stories/${other.id}`, { data: { title: 'Older Codex story visited again' } })
  expect(touched.ok(), await touched.text()).toBe(true)
  expect(await readConfig(other.id)).toEqual(originalOther)
  const fresh = await createStory(request, 'Use the remembered Native default')
  expect((await readConfig(fresh.id)).runtime.kind).toBe('native')
  expect((await readConfig(fresh.id)).runtime_capabilities.goal).toBe(false)
})

test('A runtime switch updates an existing Project override without changing user defaults', async ({ request }) => {
  const book = await createAndOpenBook(request, 'Project runtime defaults')
  const base = `/api/projects/${book.projectId}`
  const preferences = { selected: 'codex', codex: { model: 'project-codex-model' } }
  const before = await (await request.get('/api/settings')).json()
  const seeded = await request.patch(`${base}/settings`, { data: { layer: 'workspace', changes: { agent_runtimes: { ide: preferences } } } })
  expect(seeded.ok(), await seeded.text()).toBe(true)
  const created = await request.post(`${base}/agent-chat/sessions`, { data: { title: 'Project override' } })
  expect(created.ok(), await created.text()).toBe(true)
  const sessionID = (await created.json()).id
  const configPath = `${base}/conversation-config`
  const initial = await (await request.get(`${configPath}?mode=agent_chat&session_id=${sessionID}`)).json()
  expect(initial.runtime.kind).toBe('codex')
  const changed = await request.patch(configPath, { data: { binding: { mode: 'agent_chat', session_id: sessionID }, base_revision: initial.revision,
    changes: { runtime: { kind: 'native' } },
  } })
  expect(changed.ok(), await changed.text()).toBe(true)
  const saved = await (await request.get(`${base}/settings`)).json()
  expect(saved.workspace.agent_runtimes.ide).toEqual({ ...preferences, selected: 'native' })
  expect(saved.user.agent_runtimes).toEqual(before.user.agent_runtimes)
  const fresh = await request.post(`${base}/agent-chat/sessions`, { data: { title: 'Remember Project runtime' } })
  expect(fresh.ok(), await fresh.text()).toBe(true)
  const freshID = (await fresh.json()).id
  expect((await (await request.get(`${configPath}?mode=agent_chat&session_id=${freshID}`)).json()).runtime.kind).toBe('native')
})
