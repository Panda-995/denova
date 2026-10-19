import { expect, test } from '../support/fixtures'
import { createAndOpenBook } from '../support/api'

for (const theme of ['dark', 'light']) {
  test(`market batches conflict choices and reviews them before applying in ${theme}`, async ({ page, request }) => {
    await request.patch('/api/settings', { data: { layer: 'user', changes: { language: 'en-US', theme } } })
    const book = await createAndOpenBook(request, 'Update destination')
    await page.addInitScript(() => { localStorage.setItem('nova:mode', 'market'); localStorage.setItem('nova.locale.configured', 'en-US') })
    const source = { kind: 'github', url: 'https://github.com/author/resources', ref: 'main', path: 'story' }
    const entry = { id: 'story', name: { 'en-US': 'Story resources' }, description: { 'en-US': 'Shared writing and game resources' }, author: 'Author', kinds: ['lore.collection'], tags: ['writing', 'game'], format: 'denova.resource-pack', updated_at: '2026-09-30', source }
    const installation = { installation_id: 'installed', package: { id: 'story', name: 'Story resources' }, source, tracking: 'tracked', update_mode: 'manual', project_id: book.projectId, bindings: [
      { resource_id: 'lore', local: { kind: 'lore.collection', scope: 'project', project_id: book.projectId, id: 'local' }, ownership: 'owned', source_digest: 'before' },
      { resource_id: 'image', local: { kind: 'preset.image', scope: 'global', id: 'image' }, ownership: 'owned', source_digest: 'before' },
      { resource_id: 'opening', local: { kind: 'game.openings', scope: 'project', project_id: book.projectId, id: 'opening' }, ownership: 'owned', source_digest: 'before' },
    ] }
    const other = { ...installation, installation_id: 'elsewhere', project_id: 'another-project' }
    const preview = { preview_id: 'update-preview', source, candidates: [{ candidate_id: 'candidate', package: installation.package, format: 'denova.resource-pack', resources: [
      { id: 'lore', name: 'Characters', kind: 'lore.collection', path: 'lore.json', digest: 'after' },
      { id: 'image', name: 'Game image preset', kind: 'preset.image', path: 'image.json', digest: 'after' },
      { id: 'opening', name: 'Dependent opening', kind: 'game.openings', path: 'opening.json', digest: 'after', requires: ['lore'] },
    ] }] }
    const name = 'CharacterWithAVeryLongNameWhoseLocalAndUpstreamDescriptionsBothChanged'
    let plans = 0, applied = '', checked = ''
    await page.route('**/api/resource-market/catalog', route => route.fulfill({ json: { schema_version: 1, entries: [entry] } }))
    await page.route('**/api/resource-exchange/installations', route => route.fulfill({ json: [other, installation] }))
    await page.route('**/api/resource-exchange/installations/*/check', route => {
      checked = route.request().url()
      return route.fulfill({ json: preview })
    })
    await page.route('**/api/resource-exchange/previews/update-preview', route => route.fulfill({ json: {} }))
    await page.route('**/api/resource-exchange/plans', route => {
      const body = route.request().postDataJSON()
      expect(body.installation_id).toBe('installed')
      expect(body.project_id).toBe(book.projectId)
      const resolution = body.resolutions?.lore?.hero
      const imageResolution = body.resolutions?.image?.['']
      const openingResolution = body.resolutions?.opening?.arrival
      plans++
      if (plans === 2) expect(body.resolutions).toEqual({ lore: { hero: 'keep' }, image: { '': 'remote' } })
      if (plans === 3) expect(body.resolutions).toEqual({ lore: { hero: 'keep' }, image: { '': 'remote' }, opening: { arrival: 'remote' } })
      if (plans === 4) {
        expect(body.resolutions).toBeUndefined()
        return route.fulfill({ json: { plan_id: 'unchanged-plan', items: [], installation, updates: [
          { resource_id: 'lore', name: 'Characters', state: 'unchanged' },
          { resource_id: 'image', name: 'Game image preset', state: 'unchanged' },
          { resource_id: 'opening', name: 'Dependent opening', state: 'unchanged' },
        ] } })
      }
      return route.fulfill({ json: { plan_id: `plan-${plans}`, items: [], installation, updates: [
        { resource_id: 'lore', member_id: 'world', name: 'World setting', state: 'update' },
        { resource_id: 'lore', member_id: 'hero', name, state: resolution === 'keep' ? 'keep' : resolution === 'remote' ? 'update' : 'conflict', conflict: true, resolution },
        { resource_id: 'image', name: 'Game image preset', state: imageResolution === 'keep' ? 'keep' : imageResolution === 'remote' ? 'update' : 'conflict', conflict: true, resolution: imageResolution },
        resolution
          ? { resource_id: 'opening', member_id: 'arrival', name: 'Dependent opening', state: openingResolution === 'keep' ? 'keep' : openingResolution === 'remote' ? 'update' : 'conflict', conflict: true, resolution: openingResolution }
          : { resource_id: 'opening', name: 'Dependent opening', state: 'blocked' },
      ] } })
    })
    await page.route('**/api/resource-exchange/plans/*/apply', route => { applied = route.request().url(); return route.fulfill({ json: installation }) })
    await page.goto('/')
    await page.getByRole('button', { name: 'Story resources', exact: true }).click()
    const detail = page.getByTestId('market-entry-detail')
    await expect(detail.getByRole('combobox', { name: 'Update destination' })).toContainText(book.title)
    await detail.getByRole('button', { name: 'Check and update installed package' }).click()
    expect(checked).toContain('/installed/check')
    const dialog = page.getByRole('dialog')
    await dialog.getByRole('button', { name: 'Review plan', exact: true }).click()
    await expect(dialog.getByText('Both sides changed', { exact: true })).toHaveCount(2)
    const resolution = dialog.getByRole('combobox', { name: `Resolve conflict for ${name}` })
    const imageResolution = dialog.getByRole('combobox', { name: 'Resolve conflict for Game image preset' })
    await dialog.getByRole('button', { name: 'Replace all', exact: true }).click()
    await expect(resolution).toContainText('Back up and use upstream')
    await expect(imageResolution).toContainText('Back up and use upstream')
    await expect(dialog.getByRole('button', { name: 'Apply this update', exact: true })).toHaveCount(0)
    await dialog.getByRole('button', { name: 'Keep all local content', exact: true }).click()
    await expect(resolution).toContainText('Keep my content')
    await expect(imageResolution).toContainText('Keep my content')
    await dialog.getByRole('button', { name: 'Leave all pending', exact: true }).click()
    await expect(resolution).toContainText('Leave pending')
    await expect(imageResolution).toContainText('Leave pending')
    await expect(dialog.getByRole('button', { name: 'Apply this update', exact: true })).toBeVisible()
    await dialog.getByRole('button', { name: 'Replace all', exact: true }).click()
    await resolution.click()
    await page.getByRole('option', { name: 'Keep my content and continue tracking' }).click()
    await expect(resolution).toContainText('Keep my content')
    await expect(imageResolution).toContainText('Back up and use upstream')
    await expect(dialog.getByText('Conflicts: 2 · Replace: 1 · Keep local: 1 · Pending: 0', { exact: true })).toBeVisible()
    expect(plans).toBe(1)
    expect(applied).toBe('')
    for (const width of [1440, 390]) {
      await page.setViewportSize({ width, height: 900 })
      await expect.poll(() => dialog.evaluate(element => {
        const bounds = element.getBoundingClientRect()
        return { fits: element.scrollWidth <= element.clientWidth, width: element.clientWidth, scroll: element.scrollWidth, overflow: [...element.querySelectorAll('*')].filter(child => child.getBoundingClientRect().right > bounds.right + 1).map(child => [child.tagName, child.className]) }
      })).toMatchObject({ fits: true, overflow: [] })
      await page.screenshot({ path: `test-results/market-update-${theme}-${width}.png`, fullPage: true })
    }
    await dialog.getByRole('button', { name: 'Update installation plan', exact: true }).click()
    await expect(dialog.getByRole('combobox', { name: 'Resolve conflict for Dependent opening' })).toContainText('Leave pending')
    await expect(dialog.getByRole('button', { name: 'Apply this update', exact: true })).toBeVisible()
    expect(plans).toBe(2)
    expect(applied).toBe('')
    // Returning to the checked choice needs no new plan.
    await resolution.click()
    await page.getByRole('option', { name: 'Leave pending, keep local content' }).click()
    await expect(dialog.getByRole('button', { name: 'Update installation plan', exact: true })).toBeVisible()
    await resolution.click()
    await page.getByRole('option', { name: 'Keep my content and continue tracking' }).click()
    await expect(dialog.getByRole('button', { name: 'Apply this update', exact: true })).toBeVisible()
    expect(plans).toBe(2)
    await dialog.getByRole('combobox', { name: 'Resolve conflict for Dependent opening' }).click()
    await page.getByRole('option', { name: 'Back up and use upstream content' }).click()
    await dialog.getByRole('button', { name: 'Update installation plan', exact: true }).click()
    await expect(dialog.getByRole('button', { name: 'Apply this update', exact: true })).toBeVisible()
    expect(plans).toBe(3)
    await dialog.getByRole('button', { name: 'Apply this update', exact: true }).click()
    await expect(dialog).toBeHidden()
    expect(applied).toContain('/plan-3/apply')
    // A fresh review has no leftover choices or bulk controls when there are no conflicts.
    await page.reload()
    await page.getByRole('button', { name: 'Story resources', exact: true }).click()
    await detail.getByRole('button', { name: 'Check and update installed package' }).click()
    await dialog.getByRole('button', { name: 'Review plan', exact: true }).click()
    await expect(dialog.getByText('Already matches', { exact: true })).toHaveCount(3)
    await expect(dialog.getByRole('group', { name: 'Resolve all conflicts' })).toHaveCount(0)
    await expect(dialog.getByRole('combobox')).toHaveCount(0)
    await expect(dialog.getByRole('button', { name: 'Apply this update', exact: true })).toBeVisible()
    expect(await dialog.evaluate(element => element.scrollWidth <= element.clientWidth)).toBe(true)
    await dialog.getByRole('button', { name: 'Close', exact: true }).click()
  })
}
