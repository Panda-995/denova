import { expect, test } from '../support/fixtures'

for (const [theme, language] of [['dark', 'zh-CN'], ['light', 'en-US']]) {
  test(`market supports browser downloads and file fallback in ${theme}`, async ({ page, request, browserDiagnostics }) => {
    await request.patch('/api/settings', { data: { layer: 'user', changes: { language, theme } } })
    const chinese = language === 'zh-CN'
    const labels = chinese ? {
      add: '从本地文件添加', update: '从本地文件更新', online: '另行导入资源包',
      file: '本地文件', download: '通过浏览器下载 ZIP', preview: '预览资源',
      review: '生成安装计划', apply: '应用本次更新', cancel: '取消',
    } : {
      add: 'Add from a local file', update: 'Update from a local file', online: 'Import another copy',
      file: 'Local file', download: 'Download ZIP in browser', preview: 'Preview resources',
      review: 'Review plan', apply: 'Apply this update', cancel: 'Cancel',
    }
    const source = { kind: 'github', url: 'https://github.com/author/resources', ref: 'main', path: 'examples/story' }
    const pkg = { id: 'story', name: 'Story resources' }
    const installation = { installation_id: 'installed', package: pkg, source, tracking: 'tracked', update_mode: 'notify', bindings: [
      { resource_id: 'image', local: { kind: 'preset.image', scope: 'global', id: 'image' }, ownership: 'owned', source_digest: 'before' },
    ] }
    const candidate = { candidate_id: 'examples/story', package: pkg, format: 'denova.resource-pack', resources: [
      { id: 'image', name: 'Story image preset', kind: 'preset.image', path: 'image.json', digest: 'after' },
    ] }
    const preview = { preview_id: 'file-preview', source: { kind: 'file', filename: 'resources-main.zip' }, candidates: [
      { ...candidate, candidate_id: 'examples/other', package: { id: 'other', name: 'Other package' } }, candidate,
    ] }
    await page.addInitScript(language => {
      localStorage.setItem('nova:mode', 'market')
      localStorage.setItem('nova.locale.configured', language)
    }, language)
    await page.route('**/api/resource-market/catalog', route => route.fulfill({ json: { schema_version: 1, entries: [
      { id: pkg.id, name: { 'en-US': pkg.name }, description: { 'en-US': 'Writing and game resources' }, author: 'Author', format: candidate.format, kinds: ['preset.image'], tags: [], updated_at: '2026-10-11', source },
    ] } }))
    await page.route('**/api/resource-exchange/installations', route => route.fulfill({ json: [installation] }))
    let uploads = 0, online = 0
    browserDiagnostics.allow(/console\.error:.*502.*\/api\/resource-exchange\/previews/)
    browserDiagnostics.allow(/http\.5xx: POST .*\/api\/resource-exchange\/previews returned 502/)
    browserDiagnostics.allow(/console\.error: \[market\] import operation failed.*GitHub download failed/)
    await page.route('**/api/resource-exchange/previews', route => {
      if (route.request().headers()['content-type'].startsWith('multipart/form-data')) {
        uploads++
        return route.fulfill({ json: preview })
      }
      online++
      return route.fulfill({ status: 502, json: { error: 'GitHub download failed', code: 'market.errors.downloadFailed' } })
    })
    await page.route('**/api/resource-exchange/previews/file-preview', route => route.fulfill({ json: {} }))
    await page.route('**/api/resource-exchange/plans', route => {
      expect(route.request().postDataJSON()).toMatchObject({ installation_id: installation.installation_id, candidate_id: candidate.candidate_id, resources: ['image'], update_mode: 'notify' })
      return route.fulfill({ json: { plan_id: 'file-plan', items: [], installation, updates: [{ resource_id: 'image', name: 'Story image preset', state: 'update' }] } })
    })
    await page.route('**/api/resource-exchange/plans/file-plan/apply', route => route.fulfill({ json: installation }))
    await page.goto('/')
    await page.getByRole('button', { name: pkg.name, exact: true }).click()
    const detail = page.getByTestId('market-entry-detail')
    await detail.getByRole('button', { name: labels.add, exact: true }).click()
    const dialog = page.getByRole('dialog')
    await expect(dialog.getByRole('radio', { name: labels.file, exact: true })).toBeChecked()
    await expect(dialog.getByRole('link', { name: labels.download })).toHaveAttribute('href', `${source.url}/archive/main.zip`)
    await expect(dialog.getByText(/denova-pack\.json/)).toBeVisible()
    await expect(dialog.getByText(/examples\/story/)).toBeVisible()
    expect(online).toBe(0)
    for (const width of [390, 1440]) {
      await page.setViewportSize({ width, height: 900 })
      expect(await dialog.evaluate(element => element.scrollWidth <= element.clientWidth)).toBe(true)
      await page.screenshot({ path: `test-results/market-file-${theme}-${width}.png`, fullPage: true })
    }
    await dialog.getByLabel(labels.file, { exact: true }).setInputFiles({ name: 'resources-main.zip', mimeType: 'application/zip', buffer: Buffer.from('mocked archive') })
    await dialog.getByRole('button', { name: labels.preview, exact: true }).click()
    await expect(dialog.getByRole('combobox').first()).toContainText(pkg.name)
    await dialog.getByRole('button', { name: labels.cancel, exact: true }).click()
    await detail.getByRole('button', { name: labels.online, exact: true }).click()
    await expect(dialog.getByRole('alert')).toContainText('GitHub download failed')
    expect(online).toBe(1)
    await dialog.getByRole('radio', { name: labels.file, exact: true }).click()
    await expect(dialog.getByLabel(labels.file, { exact: true })).toBeVisible()
    await expect(dialog.getByRole('alert')).toHaveCount(0)
    await dialog.getByRole('button', { name: labels.cancel, exact: true }).click()
    // Update targets the existing installation and selects its package in a repository ZIP.
    await detail.getByRole('button', { name: labels.update, exact: true }).click()
    await expect(dialog.getByRole('radio', { name: labels.file, exact: true })).toBeChecked()
    await dialog.getByLabel(labels.file, { exact: true }).setInputFiles({ name: 'resources-main.zip', mimeType: 'application/zip', buffer: Buffer.from('mocked archive') })
    await dialog.getByRole('button', { name: labels.preview, exact: true }).click()
    await expect(dialog.getByRole('combobox')).toContainText(pkg.name)
    await dialog.getByRole('button', { name: labels.review, exact: true }).click()
    await dialog.getByRole('button', { name: labels.apply, exact: true }).click()
    await expect(dialog).toBeHidden()
    expect(uploads).toBe(2)
    expect(online).toBe(1)
  })
}
