import { expect, test } from '../support/fixtures'
import en from '../../src/i18n/locales/en-US/market'
import zh from '../../src/i18n/locales/zh-CN/market'
import type { Installation, ResourceKind } from '../../src/features/market/api'

for (const language of ['en-US', 'zh-CN']) {
  for (const theme of ['dark', 'light']) {
    test(`installed packages remain usable with long content in ${language} ${theme}`, async ({ page, request }) => {
      const labels = language === 'en-US' ? en : zh
      await request.patch('/api/settings', { data: { layer: 'user', changes: { language, theme } } })
      await page.addInitScript((locale) => {
        localStorage.setItem('nova:mode', 'market')
        localStorage.setItem('nova.locale.configured', locale)
      }, language)
      const kinds: ResourceKind[] = ['lore.collection', 'game.openings', 'preset.narrative', 'preset.image', 'skill', 'preset.actor_state', 'preset.rules', 'extension.game']
      const installed: Installation = {
        installation_id: 'mixed-package',
        package: { id: 'shared', name: language === 'en-US' ? 'Shared writing and game resources with a long package name' : '写作与游戏共用资源包 · 包含世界资料、开场、创作方案与游戏扩展', description: (language === 'en-US' ? 'A shared writing and game resource collection. ' : '包含写作与游戏共用的资料和创作方案。').repeat(12), version: '2.1.0' },
        source: { kind: 'github', url: `https://github.com/author/${'long-repository-name-'.repeat(8)}` },
        tracking: 'tracked', update_mode: 'manual', local_state: 'modified', remote_state: 'update_available',
        updated_at: '2026-09-30T00:00:00Z',
        bindings: kinds.map((kind, index) => ({
          resource_id: `resource-${index}`, source_digest: 'original',
          local: { kind, scope: 'global', id: index === 0 ? 'long-resource-id-'.repeat(20) : `resource-${index}` },
          ownership: index === 7 ? 'reference' : 'owned', upstream_removed: index === 0,
        })),
      }
      const localFile: Installation = {
        ...installed, installation_id: 'local-file', package: { id: 'file', name: 'Local package' },
        source: { kind: 'file', filename: `${'local-package-'.repeat(15)}.zip` },
        remote_state: '', local_state: 'unchanged', bindings: [installed.bindings[4]],
      }
      let installations = [installed, localFile]
      let changedPolicy = '', checkedInstallation = ''
      await page.route('**/api/resource-market/catalog*', route => route.fulfill({ json: { schema_version: 1, entries: [] } }))
      await page.route('**/api/resource-exchange/installations', route => route.fulfill({ json: installations }))
      await page.route('**/api/resource-exchange/installations/mixed-package/policy', route => {
        changedPolicy = route.request().postDataJSON().update_mode
        installed.update_mode = changedPolicy
        return route.fulfill({ json: installed })
      })
      await page.route('**/api/resource-exchange/installations/mixed-package/check', route => {
        checkedInstallation = route.request().url()
        return route.fulfill({ json: {
          preview_id: 'installed-layout-preview', source: installed.source, candidates: [{
            candidate_id: 'mixed', package: installed.package, format: 'denova.resource-pack',
            resources: [{ id: 'lore', kind: 'lore.collection', name: 'World', path: 'lore.json', digest: 'new' }],
          }],
        } })
      })
      await page.route('**/api/resource-exchange/previews/installed-layout-preview', route => route.fulfill({ json: {} }))
      await page.route('**/api/resource-exchange/installations/mixed-package/detach', route => {
        installed.tracking = 'detached'
        return route.fulfill({ json: installed })
      })
      await page.setViewportSize({ width: 1440, height: 1000 })
      await page.goto('/')
      const market = page.getByTestId('resource-market')
      const navigation = page.getByRole('navigation', { name: labels['market.navigation'], exact: true })
      await navigation.getByRole('button', { name: labels['market.acquired'], exact: true }).click()
      const card = market.getByTestId('market-installation-card').filter({ hasText: installed.package.name })
      const trigger = card.getByRole('button', { name: installed.package.name, exact: true })
      const list = market.getByTestId('market-installation-list')
      const detail = market.getByTestId('market-installation-detail')
      const fileCard = market.getByTestId('market-installation-card').filter({ hasText: localFile.package.name })
      for (const width of [1440, 640, 390]) {
        await page.setViewportSize({ width, height: 1000 })
        const first = await card.boundingBox(), second = await fileCard.boundingBox()
        expect(first!.height).toBeCloseTo(second!.height, 0)
        if (width !== 390) expect(first!.y).toBeCloseTo(second!.y, 0)
        expect(await list.evaluate(element => element.scrollWidth <= element.clientWidth)).toBe(true)
        await list.screenshot({ path: `test-results/market-installed-list-${language}-${theme}-${width}.png` })
      }
      // Footer and padding open the same detail as keyboard activation of the title.
      await card.click({ position: { x: 12, y: (await card.boundingBox())!.height - 12 } })
      await expect(list).toBeHidden()
      await expect(detail.getByRole('heading', { name: installed.package.name, exact: true })).toBeFocused()
      await expect(detail.getByRole('listitem')).toHaveCount(kinds.length)
      await expect(detail.getByText(labels['market.states.upstream_removed'], { exact: true })).toBeVisible()
      await expect(detail.getByText(labels['market.actions.reference'], { exact: true })).toBeVisible()
      await detail.getByRole('button', { name: labels['market.back'], exact: true }).click()
      await expect(trigger).toBeFocused()
      await trigger.press('Enter')
      await expect(detail).toBeVisible()
      await detail.getByRole('button', { name: labels['market.back'], exact: true }).click()
      await trigger.press('Space')
      await expect(detail).toBeVisible()

      for (const width of [1440, 768, 390]) {
        await page.setViewportSize({ width, height: 1000 })
        expect(await detail.evaluate(element => {
          const bounds = element.getBoundingClientRect()
          const sections = [element, ...element.querySelectorAll('article, header, section, aside, [data-slot="field"]')]
          return bounds.left >= 0 && bounds.right <= window.innerWidth
            && sections.every(section => section.scrollWidth <= section.clientWidth)
        })).toBe(true)
        if (width !== 768) await detail.screenshot({ path: `test-results/market-installed-${language}-${theme}-${width}.png` })
      }
      const policy = detail.getByRole('combobox', { name: labels['market.updatePolicy'] })
      await policy.click()
      await expect(page.getByRole('option', { name: labels['market.policy.auto_apply'], exact: true })).toHaveCount(0)
      await page.getByRole('option', { name: labels['market.policy.notify'], exact: true }).click()
      await expect.poll(() => changedPolicy).toBe('notify')
      await expect(policy).toContainText(labels['market.policy.notify'])

      await detail.getByRole('button', { name: labels['market.checkUpdate'], exact: true }).click()
      await expect(page.getByRole('dialog')).toBeVisible()
      expect(checkedInstallation).toContain('/mixed-package/check')
      await page.getByRole('dialog').press('Escape')
      await expect(page.getByRole('dialog')).toBeHidden()

      await detail.getByRole('button', { name: labels['market.acquired.actions'], exact: true }).click()
      await expect(page.getByRole('menuitem', { name: labels['market.export.title'], exact: true })).toBeVisible()
      await expect(page.getByRole('menuitem', { name: labels['market.backups.title'], exact: true })).toBeVisible()
      await page.getByRole('menuitem', { name: labels['market.detach'], exact: true }).click()
      await expect(detail.getByText(labels['market.states.detached'], { exact: true })).toBeVisible()
      await expect(policy).toHaveCount(0)
      await expect(detail.getByRole('button', { name: labels['market.checkUpdate'], exact: true })).toHaveCount(0)
      await detail.getByRole('button', { name: labels['market.acquired.actions'], exact: true }).click()
      await expect(page.getByRole('menuitem', { name: labels['market.detach'], exact: true })).toHaveCount(0)
      await page.getByRole('menu').press('Escape')
      await detail.getByRole('button', { name: labels['market.back'], exact: true }).click()
      await expect(trigger).toBeFocused()

      await fileCard.getByRole('button', { name: localFile.package.name, exact: true }).click()
      await expect(detail.getByText(localFile.source.filename!, { exact: true })).toBeVisible()
      await expect(detail.getByRole('combobox')).toHaveCount(0)
      await expect(detail.getByRole('button', { name: labels['market.checkUpdate'], exact: true })).toHaveCount(0)
      await detail.getByRole('button', { name: labels['market.back'], exact: true }).click()

      installations = []
      await market.getByRole('button', { name: labels['market.checkUpdates'], exact: true }).click()
      await expect(market.getByText(labels['market.acquiredEmpty'], { exact: true })).toBeVisible()
      expect(await market.evaluate(element => element.scrollWidth <= element.clientWidth)).toBe(true)
      await market.getByRole('button', { name: labels['market.discover'], exact: true }).click()
      await expect(market.getByRole('heading', { name: labels['market.discover'], exact: true })).toBeVisible()
    })
  }
}

test('package detail preserves list position and stays open when an update leaves the list', async ({ page, request }) => {
  await request.patch('/api/settings', { data: { layer: 'user', changes: { language: 'en-US' } } })
  await page.addInitScript(() => {
    localStorage.setItem('nova:mode', 'market')
    localStorage.setItem('nova.locale.configured', 'en-US')
  })
  const installations: Installation[] = Array.from({ length: 30 }, (_, index) => ({
    installation_id: `package-${index}`, package: { id: `package-${index}`, name: `Package ${index}` },
    source: { kind: 'github', url: 'https://github.com/author/resources' }, tracking: 'tracked',
    update_mode: 'manual', local_state: 'unchanged', remote_state: 'update_available', updated_at: '',
    bindings: [{ resource_id: 'skill', local: { kind: 'skill', scope: 'global', id: 'skill' }, ownership: 'owned', source_digest: 'original' }],
  }))
  await page.route('**/api/resource-market/catalog*', route => route.fulfill({ json: { schema_version: 1, entries: [] } }))
  await page.route('**/api/resource-exchange/installations', route => route.fulfill({ json: installations }))
  await page.route('**/api/resource-exchange/installations/package-29/policy', route => {
    for (const item of installations) item.remote_state = ''
    installations[29].update_mode = 'notify'
    return route.fulfill({ json: installations[29] })
  })
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.goto('/')
  const navigation = page.getByRole('navigation', { name: en['market.navigation'], exact: true })
  await navigation.getByRole('button', { name: en['market.acquired'], exact: true }).click()
  const list = page.getByTestId('market-installation-list')
  const trigger = list.getByRole('button', { name: 'Package 29', exact: true })
  await trigger.scrollIntoViewIfNeeded()
  const scrollTop = await list.evaluate(element => element.scrollTop)
  expect(scrollTop).toBeGreaterThan(500)
  await trigger.click()
  const detail = page.getByTestId('market-installation-detail')
  await detail.getByRole('button', { name: en['market.back'], exact: true }).click()
  await expect(trigger).toBeFocused()
  expect(await list.evaluate(element => element.scrollTop)).toBeCloseTo(scrollTop, 0)
  await navigation.getByRole('button', { name: en['market.updates'], exact: true }).click()
  await trigger.click()
  await detail.getByRole('combobox', { name: en['market.updatePolicy'] }).click()
  await page.getByRole('option', { name: en['market.policy.notify'], exact: true }).click()
  await expect(detail.getByRole('combobox')).toContainText(en['market.policy.notify'])
  await expect(detail.getByRole('heading', { name: 'Package 29', exact: true })).toBeVisible()
  await expect(navigation.getByRole('button', { name: en['market.updates'], exact: true })).toHaveAttribute('aria-current', 'page')
  await detail.getByRole('button', { name: en['market.back'], exact: true }).click()
  await expect(list.getByText(en['market.updatesEmpty'], { exact: true })).toBeVisible()
  await expect(list.getByRole('heading', { name: en['market.updates'], exact: true })).toBeFocused()
})
