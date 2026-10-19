import { expect, test } from '../support/fixtures'

for (const scenario of [
  { name: 'accepts recovered index revision conflicts', path: '/api/projects/probe/book/lore/index', code: 'api.resource.revisionConflict', recover: true, diagnosticExpected: false },
  { name: 'rejects unrecovered index revision conflicts', path: '/api/projects/probe/book/lore/index', code: 'api.resource.revisionConflict', recover: false, diagnosticExpected: true },
  { name: 'rejects untyped index conflicts even after a successful write', path: '/api/projects/probe/book/lore/index', code: 'unexpected_conflict', recover: true, diagnosticExpected: true },
  { name: 'rejects conflicts outside the index recovery flow', path: '/api/projects/probe/book/lore/items/probe', code: 'api.resource.revisionConflict', recover: true, diagnosticExpected: true },
]) {
  test(scenario.name, async ({ page }) => {
    // Negative cases must fail in the automatic diagnostics fixture.
    test.fail(scenario.diagnosticExpected, 'Unexpected or unrecovered conflicts must remain test failures')
    await page.route('**/diagnostic-probe', route => route.fulfill({
      contentType: 'text/html', body: '<html><body>Diagnostic probe</body></html>',
    }))
    let writes = 0
    await page.route(`**${scenario.path}`, route => route.fulfill({
      status: ++writes === 1 ? 409 : 200,
      json: writes === 1 ? { code: scenario.code } : { saved: true },
    }))
    await page.goto('/diagnostic-probe')
    const statuses = await page.evaluate(async ({ path, recover }) => {
      const results = [(await fetch(path, { method: 'PUT' })).status]
      if (recover) results.push((await fetch(path, { method: 'PUT' })).status)
      return results
    }, scenario)
    expect(statuses).toEqual(scenario.recover ? [409, 200] : [409])
  })
}
