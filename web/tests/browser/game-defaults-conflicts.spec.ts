import { expect, test } from '../support/fixtures'
import { createAndOpenBook } from '../support/api'

for (const [locale, theme] of [['zh-CN', 'dark'], ['en-US', 'light']]) {
  test(`background adoption waits for conflict review in ${locale}`, async ({ page, request }) => {
    const zh = locale === 'zh-CN'
    await request.patch('/api/settings', { data: { layer: 'user', changes: { language: locale, theme } } })
    const book = await createAndOpenBook(request, 'Background conflicts')
    const source = { kind: 'github', url: 'https://github.com/example/world', ref: 'main' }
    const name = 'Background package'
    const entry = { id: 'world', name: { [locale]: name }, description: { [locale]: name }, author: 'Fixture', kinds: ['lore.collection'], format: 'denova.resource-pack', tags: [], source, updated_at: '2026-09-30' }
    const installation = { installation_id: 'installed', package: { id: 'world', name }, source, tracking: 'tracked', update_mode: 'manual', project_id: book.projectId, bindings: [
      { resource_id: 'lore', local: { kind: 'lore.collection', scope: 'project', project_id: book.projectId, id: 'local' }, ownership: 'owned', source_digest: 'before' },
    ] }
    const preview = { preview_id: 'background-preview', source, candidates: [{ candidate_id: '.', package: installation.package, format: 'denova.resource-pack', resources: [
      { id: 'lore', name: 'World', kind: 'lore.collection', path: 'lore.json', digest: 'after' },
    ], game_defaults: { default_background: { resource_id: 'lore', item_id: 'world', asset_path: 'background.png' } } }] }
    let plans = 0, applied = false
    await page.route('**/api/resource-market/catalog', route => route.fulfill({ json: { schema_version: 1, entries: [entry] } }))
    await page.route('**/api/resource-exchange/installations', route => route.fulfill({ json: [installation] }))
    await page.route('**/api/resource-exchange/installations/installed/check', route => route.fulfill({ json: preview }))
    await page.route('**/api/resource-exchange/previews/background-preview', route => route.fulfill({ json: {} }))
    await page.route('**/api/resource-exchange/plans', route => {
      const body = route.request().postDataJSON()
      expect(body.game_defaults_fields).toEqual(['default_background'])
      const resolution = body.resolutions?.lore?.world
      plans++
      return route.fulfill({ json: { plan_id: `plan-${plans}`, installation, items: [],
        updates: [{ resource_id: 'lore', member_id: 'world', name: 'World', state: resolution === 'remote' ? 'update' : 'conflict', conflict: true, resolution }],
        game_defaults_unavailable: resolution === 'remote' ? [] : ['default_background'],
        ...(resolution === 'remote' ? { game_defaults_applied: { default_background: { mode: 'image', item_id: 'world', asset_id: 'background' } } } : {}),
      } })
    })
    await page.route('**/api/resource-exchange/plans/*/apply', route => { applied = true; return route.fulfill({ json: installation }) })
    await page.addInitScript(locale => { localStorage.setItem('nova:mode', 'market'); localStorage.setItem('nova.locale.configured', locale) }, locale)
    await page.goto('/')
    await page.getByRole('button', { name, exact: true }).click()
    await page.getByTestId('market-entry-detail').getByRole('button', { name: zh ? '检查并更新已有安装' : 'Check and update installed package' }).click()
    const dialog = page.getByRole('dialog')
    const adopt = dialog.getByRole('checkbox', { name: zh ? '用作本书的新故事默认配置' : 'Use as new-story defaults for this book', exact: true })
    await expect(adopt).toBeEnabled()
    await adopt.check()
    await dialog.getByRole('button', { name: zh ? '生成安装计划' : 'Review plan', exact: true }).click()
    const status = dialog.getByText(zh ? /^所选默认配置暂不可用：默认背景/ : /^Selected defaults are unavailable: Default background/)
    await expect(status).toBeVisible()
    const apply = dialog.getByRole('button', { name: zh ? '应用本次更新' : 'Apply this update', exact: true })
    await expect(apply).toBeDisabled()
    expect(plans).toBe(1)
    expect(applied).toBe(false)
    for (const width of [1440, 390]) {
      await page.setViewportSize({ width, height: 900 })
      await status.scrollIntoViewIfNeeded()
      expect(await dialog.evaluate(element => element.scrollWidth <= element.clientWidth)).toBe(true)
      await page.screenshot({ path: test.info().outputPath(`background-conflict-${locale}-${width}.png`) })
    }
    await dialog.getByRole('button', { name: zh ? '全部替换' : 'Replace all', exact: true }).click()
    await dialog.getByRole('button', { name: zh ? '更新安装计划' : 'Update installation plan', exact: true }).click()
    await expect(status).toBeHidden()
    await expect(apply).toBeEnabled()
    expect(plans).toBe(2)
    expect(applied).toBe(false)
    await apply.click()
    await expect(dialog).toBeHidden()
    expect(applied).toBe(true)
  })
}
