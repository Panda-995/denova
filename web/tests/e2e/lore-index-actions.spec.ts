import { expect, test } from '../support/fixtures'
import { createAndOpenBook, createStartedStory } from '../support/api'
import type { LoreItem } from '../../src/lib/api'

for (const mode of ['writing', 'game'] as const) {
  test(`edits card loading and retries only failed batch entries in ${mode}`, async ({ page, request, browserDiagnostics }, testInfo) => {
    const english = mode === 'game'
    const labels = english ? {
      sidebar: 'Workbench sidebar', surface: 'Game', library: 'Lore', more: 'More', index: 'Lore index',
      expand: 'Expand all', multiple: 'Select multiple', all: 'Select all', clear: 'Deselect all', batch: 'Batch actions', done: 'Done',
      loading: 'Loading options: Alpha', strategy: 'Load Strategy', resident: 'Resident', manual: 'Manual', status: 'Status',
      enabled: 'Enabled', disabled: 'Disabled', mixed: 'Partially enabled', detail: 'Detail in this group', brief: 'Brief', full: 'Full text',
      add: 'Add to group', remove: 'Remove from this group', selected: (count: number) => `${count} selected`,
    } : {
      sidebar: '工作台侧边栏', surface: '写作', library: '资料库', more: '更多', index: '资料索引',
      expand: '全部展开', multiple: '多选', all: '全选', clear: '取消全选', batch: '批量操作', done: '完成',
      loading: '加载选项：Alpha', strategy: '加载策略', resident: '常驻', manual: '手动', status: '状态',
      enabled: '启用', disabled: '停用', mixed: '部分启用', detail: '当前分组展示', brief: '简介', full: '全文',
      add: '加入分组', remove: '移出当前分组', selected: (count: number) => `已选 ${count} 项`,
    }
    const book = await createAndOpenBook(request, `Index actions ${mode}`)
    if (mode === 'game') await createStartedStory(request, 'Index actions game')
    const base = `/api/projects/${book.projectId}/book/lore`
    const entries = async (): Promise<LoreItem[]> => (await (await request.get(`${base}/items`)).json()).items
    const index = await (await request.get(`${base}/index`)).json()
    expect((await request.put(`${base}/index`, { data: { base_revision: index.revision, guide: {
      intro_markdown: '', groups: [
        { id: 'cast', name: 'Cast', purpose: '', body_markdown: '', default_detail: 'name' },
        { id: 'extra', name: 'Extra', purpose: '', body_markdown: '', default_detail: 'name' },
        { id: 'empty', name: 'LongGroupName'.repeat(12), purpose: '', body_markdown: '', default_detail: 'name' },
      ],
    } } })).ok()).toBe(true)
    for (const [id, name, type, memberships] of [
      ['alpha', 'Alpha', 'world', [{ group_id: 'cast', detail: 'inherit' }]],
      ['beta', 'BetaLongUnbrokenName'.repeat(6), 'world', [{ group_id: 'cast', detail: 'inherit' }]],
      ['auto-a', 'Auto A', 'character', []], ['auto-b', 'Auto B', 'character', []],
    ] as const) expect((await request.post(`${base}/items`, { data: {
      id, name, type, load_mode: 'auto', index_memberships: memberships,
      brief_description: `BRIEF_${id} ${'LongDescription'.repeat(30)}`, content: `BODY_${id}`,
      tags: ['kept tag'], keywords: ['kept keyword'],
    } })).ok()).toBe(true)
    const settings = await (await request.get('/api/settings')).json()
    await request.patch('/api/settings', { data: { layer: 'user', base_revision: settings.revisions.user, changes: { theme: english ? 'light' : 'dark', language: english ? 'en-US' : 'zh-CN' } } })
    await page.setViewportSize({ width: 1680, height: 1000 })
    await page.goto('/')
    const sidebar = page.getByLabel(labels.sidebar)
    await sidebar.getByRole('button', { name: labels.surface, exact: true }).click()
    await sidebar.getByRole('button', { name: labels.library, exact: true }).click()
    await page.getByTestId('lore-library').getByRole('button', { name: labels.more, exact: true }).click()
    await page.getByRole('menuitem', { name: labels.index, exact: true }).click()
    const doc = page.getByTestId('lore-index-document')
    await expect(doc.getByTestId('lore-index-auto-group')).toHaveCount(1)
    await doc.getByRole('button', { name: labels.expand, exact: true }).click()
    const group = (name: string) => doc.getByTestId('lore-index-section').filter({ has: page.getByRole('button', { name: english ? `Edit group: ${name}` : `编辑分组：${name}`, exact: true }) })
    const cast = group('Cast')
    const extra = group('Extra')
    const alpha = cast.getByTestId('lore-card-alpha')
    const beta = cast.getByTestId('lore-card-beta')
    const preview = doc.getByTestId('lore-index-preview')
    await expect(alpha).toBeVisible()
    const header = cast.locator(':scope > div').first()
    const multiple = header.getByRole('button', { name: labels.multiple, exact: true })
    const headerBox = (await header.boundingBox())!
    const multipleBox = (await multiple.boundingBox())!
    expect(multipleBox.x + multipleBox.width).toBeGreaterThan(headerBox.x + headerBox.width - 6)
    const collapse = cast.getByRole('button', { name: english ? 'Expand or collapse group: Cast' : '展开或收起分组：Cast', exact: true })
    await collapse.click()
    await expect(alpha).toBeHidden()
    await multiple.click()
    await expect(collapse).toHaveAttribute('aria-expanded', 'true')
    await expect(multiple).toHaveAttribute('aria-pressed', 'true')
    await expect(cast.getByText(labels.selected(0), { exact: true })).toBeVisible()
    await cast.getByRole('button', { name: labels.done, exact: true }).click()
    const cardBox = (await alpha.boundingBox())!
    const options = alpha.getByRole('button', { name: labels.loading, exact: true })
    const dialog = page.getByRole('dialog', { name: labels.loading, exact: true })
    await page.mouse.move(1, 1)
    await expect(options).toHaveCSS('opacity', '0')
    await options.focus()
    await expect(options).toHaveCSS('opacity', '1')
    await multiple.focus()
    await expect(options).toHaveCSS('opacity', '0')
    await alpha.hover()
    await expect(options).toHaveCSS('opacity', '1')
    await options.click()
    await expect(doc).toBeVisible()
    const strategy = dialog.getByRole('radiogroup', { name: labels.strategy, exact: true })
    await expect(strategy.getByRole('radio', { name: english ? 'On demand' : '按需', exact: true })).toBeChecked()
    await strategy.getByRole('radio', { name: english ? 'On demand' : '按需', exact: true }).click()
    await expect(strategy.getByRole('radio', { name: english ? 'On demand' : '按需', exact: true })).toBeChecked()
    await page.screenshot({ path: testInfo.outputPath(`index-${mode}-loading-options.png`), animations: 'disabled' })
    await strategy.getByRole('radio', { name: labels.resident, exact: true }).click()
    await expect.poll(async () => (await entries()).find(item => item.id === 'alpha')?.load_mode).toBe('resident')
    await expect(alpha).toBeVisible()
    expect((await alpha.boundingBox())!.height).toBeCloseTo(cardBox.height, 0)
    await options.click()
    await dialog.getByRole('combobox', { name: labels.detail, exact: true }).click()
    await page.getByRole('option', { name: labels.full, exact: true }).click()
    await expect(preview).toContainText('BODY_alpha')
    await options.click()
    await expect(dialog.getByRole('switch', { name: labels.status, exact: true })).toBeChecked()
    await dialog.getByRole('switch', { name: labels.status, exact: true }).click()
    await expect.poll(async () => (await entries()).find(item => item.id === 'alpha')?.enabled).toBe(false)
    await expect(preview).not.toContainText('BODY_alpha')
    await expect(alpha).toBeVisible()
    const batch = page.getByRole('dialog', { name: labels.batch, exact: true })
    await multiple.click()
    await cast.getByRole('button', { name: labels.all, exact: true }).click()
    await cast.getByRole('button', { name: labels.batch, exact: true }).click()
    await expect(batch.getByText(labels.mixed, { exact: true })).toBeVisible()
    await expect(batch.getByRole('switch', { name: labels.status, exact: true })).not.toBeChecked()
    await expect(batch.getByRole('radiogroup', { name: labels.strategy, exact: true }).getByRole('radio', { checked: true })).toHaveCount(0)
    await batch.getByRole('button', { name: labels.disabled, exact: true }).click()
    await expect.poll(async () => (await entries()).filter(item => ['alpha', 'beta'].includes(item.id)).map(item => item.enabled)).toEqual([false, false])
    await expect(cast.getByText(labels.selected(0), { exact: true })).toBeVisible()
    await cast.getByRole('button', { name: labels.all, exact: true }).click()
    await cast.getByRole('button', { name: labels.batch, exact: true }).click()
    await expect(batch.getByRole('switch', { name: labels.status, exact: true })).not.toBeChecked()
    await batch.getByRole('switch', { name: labels.status, exact: true }).click()
    await expect.poll(async () => (await entries()).filter(item => ['alpha', 'beta'].includes(item.id)).map(item => item.enabled)).toEqual([true, true])
    await expect(cast.getByText(labels.selected(0), { exact: true })).toBeVisible()
    await cast.getByRole('button', { name: labels.done, exact: true }).click()
    await expect(preview).toContainText('BODY_alpha')
    await cast.getByRole('button', { name: labels.multiple, exact: true }).click()
    await alpha.click()
    await expect(cast.getByText(labels.selected(1), { exact: true })).toBeVisible()
    await beta.getByRole('checkbox').check()
    await expect(cast.getByText(labels.selected(2), { exact: true })).toBeVisible()
    expect((await alpha.boundingBox())!.height).toBeCloseTo(cardBox.height, 0)
    await cast.getByRole('button', { name: labels.clear, exact: true }).click()
    await expect(cast.getByText(labels.selected(0), { exact: true })).toBeVisible()
    await cast.getByRole('button', { name: labels.all, exact: true }).click()
    const submitted: string[] = []
    let rejectBeta = true
    browserDiagnostics.allow(/console\.error:.*409 \(Conflict\).*\/items\/beta/)
    browserDiagnostics.allow(/console\.error: \[lore-index\] item update failed/)
    await page.route(`**${base}/items/*`, async route => {
      if (route.request().method() === 'PUT') {
        const id = route.request().url().split('/').at(-1)!
        submitted.push(id)
        if (id === 'beta' && rejectBeta) {
          rejectBeta = false
          await route.fulfill({ status: 409, json: { error: 'Test revision conflict', code: 'api.resource.revisionConflict' } })
          return
        }
      }
      await route.continue()
    })
    await cast.getByRole('button', { name: labels.batch, exact: true }).click()
    await batch.getByRole('combobox', { name: labels.detail, exact: true }).click()
    await page.getByRole('option', { name: labels.brief, exact: true }).click()
    await expect(cast.getByText(labels.selected(1), { exact: true })).toBeVisible()
    await expect(alpha.getByRole('checkbox')).not.toBeChecked()
    await expect(beta.getByRole('checkbox')).toBeChecked()
    expect(submitted).toEqual(['alpha', 'beta'])
    await page.screenshot({ path: testInfo.outputPath(`index-${mode}-partial-save.png`) })
    await batch.getByRole('combobox', { name: labels.detail, exact: true }).click()
    await page.getByRole('option', { name: labels.brief, exact: true }).click()
    await expect(cast.getByText(labels.selected(0), { exact: true })).toBeVisible()
    expect(submitted).toEqual(['alpha', 'beta', 'beta'])
    await expect(preview).toContainText('BRIEF_alpha')
    await expect(preview).toContainText('BRIEF_beta')
    await page.unroute(`**${base}/items/*`)
    await cast.getByRole('button', { name: labels.all, exact: true }).click()
    // Batch controls and option popovers remain usable at phone widths.
    for (const width of [390, 320]) {
      await page.setViewportSize({ width, height: 844 })
      await cast.getByRole('button', { name: labels.batch, exact: true }).click()
      const box = (await batch.boundingBox())!
      expect(box.x).toBeGreaterThanOrEqual(0)
      expect(box.x + box.width).toBeLessThanOrEqual(width)
      expect(await doc.evaluate(node => node.scrollWidth <= node.clientWidth)).toBe(true)
      expect(await cast.evaluate(node => node.scrollWidth <= node.clientWidth)).toBe(true)
      const narrowHeader = (await header.boundingBox())!
      const narrowMultiple = (await multiple.boundingBox())!
      expect(narrowMultiple.x + narrowMultiple.width).toBeGreaterThan(narrowHeader.x + narrowHeader.width - 6)
      expect(narrowHeader.height).toBeLessThan(60)
      const batchStrategy = batch.getByRole('radiogroup', { name: labels.strategy, exact: true })
      expect(await batchStrategy.evaluate(node => node.scrollWidth <= node.clientWidth)).toBe(true)
      for (const option of await batchStrategy.getByRole('radio').all()) await expect(option).toBeInViewport()
      await page.screenshot({ path: testInfo.outputPath(`index-${mode}-${width}-batch.png`), animations: 'disabled' })
      await batch.press('Escape')
    }
    await page.setViewportSize({ width: 1680, height: 1000 })
    await cast.getByRole('button', { name: labels.batch, exact: true }).click()
    await batch.getByRole('combobox', { name: labels.add, exact: true }).click()
    await page.getByRole('option', { name: 'Extra', exact: true }).click()
    await expect(extra.locator('[data-slot="card"]')).toHaveCount(2)
    await expect.poll(async () => (await entries()).filter(item => ['alpha', 'beta'].includes(item.id)).map(item => item.index_memberships)).toEqual([
      [{ group_id: 'cast', detail: 'brief' }, { group_id: 'extra', detail: 'inherit' }],
      [{ group_id: 'cast', detail: 'brief' }, { group_id: 'extra', detail: 'inherit' }],
    ])
    await cast.getByRole('button', { name: labels.all, exact: true }).click()
    await cast.getByRole('button', { name: labels.batch, exact: true }).click()
    await batch.getByRole('button', { name: labels.remove, exact: true }).click()
    await expect(cast.locator('[data-slot="card"]')).toHaveCount(0)
    await cast.getByRole('button', { name: labels.done, exact: true }).click()
    await expect(multiple).toHaveAttribute('aria-pressed', 'false')
    await expect(extra.locator('[data-slot="card"]')).toHaveCount(2)
    const automatic = doc.getByTestId('lore-index-auto-group').filter({ hasText: english ? 'Character · On demand' : '角色 · 按需' })
    await automatic.getByRole('button', { name: labels.multiple, exact: true }).click()
    await automatic.getByRole('button', { name: labels.all, exact: true }).click()
    await automatic.getByRole('button', { name: labels.batch, exact: true }).click()
    await batch.getByRole('radiogroup', { name: labels.strategy, exact: true }).getByRole('radio', { name: labels.manual, exact: true }).click()
    await expect.poll(async () => (await entries()).filter(item => item.id.startsWith('auto-')).map(item => item.load_mode)).toEqual(['manual', 'manual'])
    await expect(automatic).toHaveCount(0)
    await extra.getByRole('button', { name: labels.multiple, exact: true }).click()
    await extra.getByRole('button', { name: labels.all, exact: true }).click()
    await extra.getByRole('button', { name: labels.batch, exact: true }).click()
    await batch.getByRole('button', { name: labels.remove, exact: true }).click()
    await expect(extra.locator('[data-slot="card"]')).toHaveCount(0)
    await expect(preview).toContainText('BODY_alpha')
    for (const item of await entries()) {
      expect(item.content).toBe(`BODY_${item.id}`)
      expect(item.tags).toEqual(['kept tag'])
      expect(item.keywords).toEqual(['kept keyword'])
      expect(item.index_memberships ?? []).toEqual([])
    }
    await page.reload()
    await page.getByRole('button', { name: labels.index, exact: true }).click()
    await expect(doc).toBeVisible()
    await expect(preview).toContainText('BODY_alpha')
    await page.screenshot({ path: testInfo.outputPath(`index-${mode}-saved.png`) })
  })
}
