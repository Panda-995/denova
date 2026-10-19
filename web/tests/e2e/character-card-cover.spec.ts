import { crc32 } from 'node:zlib'
import { expect, test } from '../support/fixtures'
import { createAndOpenBook, createProjectFile } from '../support/api'

function characterPNG(name: string): Buffer {
  const png = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aX1sAAAAASUVORK5CYII=', 'base64')
  const text = Buffer.from(JSON.stringify({ name, description: 'A portrait test character.' })).toString('base64')
  const chunk = Buffer.from(`tEXtchara\0${text}`)
  const size = Buffer.alloc(4)
  size.writeUInt32BE(chunk.length - 4)
  const checksum = Buffer.alloc(4)
  checksum.writeUInt32BE(crc32(chunk))
  return Buffer.concat([png.subarray(0, -12), size, chunk, checksum, png.subarray(-12)])
}

test('character PNG sets lore covers while only new books receive a book cover', async ({ page, request }) => {
  const existing = await createAndOpenBook(request, 'Existing card book')
  const existingURL = `/api/projects/${existing.projectId}/book/import-character-card`
  for (const hasCover of [false, true]) {
    if (hasCover) await createProjectFile(request, existing.projectId, 'assets/covers/cover.png', 'Original book cover')
    const response = await request.post(existingURL, { multipart: {
      file: { name: 'portrait.png', mimeType: 'image/png', buffer: characterPNG(`Existing ${hasCover}`) },
      lore_classification: 'heuristic', replace_cover: 'true',
    } })
    expect(response.ok(), await response.text()).toBe(true)
    const imported = await response.json()
    expect(imported.cover_path).toBeFalsy()
    const lore = await (await request.get(`/api/projects/${existing.projectId}/book/lore/items`)).json()
    const character = lore.items.find((item: { name: string }) => item.name === `Existing ${hasCover}`)
    expect(character.materials.cover_asset_id).toBe(character.resolved_materials[0].id)
    expect(character.image.image_path).not.toBe('assets/covers/cover.png')
    const cover = await request.get(`/api/books/cover?path=${encodeURIComponent(existing.workspace)}`)
    if (hasCover) expect(await cover.text()).toBe('Original book cover')
    else expect(cover.status()).toBe(404)
  }
  const png = characterPNG('Imported portrait')
  const response = await request.post('/api/books/import-character-card', { multipart: {
    file: { name: 'portrait.png', mimeType: 'image/png', buffer: png },
    lore_classification: 'heuristic', book_title: `Portrait ${Date.now()}`,
  } })
  expect(response.ok(), await response.text()).toBe(true)
  const book = await response.json()
  expect(book.cover_path).toBe('assets/covers/cover.png')
  const cover = await request.get(`/api/books/cover?path=${encodeURIComponent(book.workspace)}`)
  expect(await cover.body()).toEqual(png)
  const items = await (await request.get(`/api/projects/${book.project_id}/book/lore/items`)).json()
  const item = items.items.find((item: { name: string }) => item.name === 'Imported portrait')
  expect(item.materials.cover_asset_id).toBe(item.resolved_materials[0].id)
  expect(item.image.image_path).not.toBe(book.cover_path)
  expect(item.content).not.toContain('![')
  await page.goto('/')
  const sidebar = page.getByLabel('工作台侧边栏')
  for (const mode of ['写作', '游戏']) {
    await sidebar.getByRole('button', { name: mode, exact: true }).click()
    await sidebar.getByRole('button', { name: '资料库', exact: true }).click()
    await expect(page.getByTestId('lore-library').getByRole('button', { name: 'Imported portrait', exact: true })).toBeVisible()
  }
  await page.getByTestId('lore-library').getByRole('button', { name: 'Imported portrait', exact: true }).click()
  const portrait = page.getByRole('img', { name: 'Imported portrait', exact: true }).first()
  await expect(portrait).toBeVisible()
  await expect.poll(() => portrait.evaluate((image: HTMLImageElement) => image.complete && image.naturalWidth > 0)).toBe(true)
  for (const theme of ['dark', 'light']) {
    const changed = await request.patch('/api/settings', { data: { layer: 'user', changes: { theme } } })
    expect(changed.ok(), await changed.text()).toBe(true)
    await page.reload()
    await expect(portrait).toBeVisible()
    await page.screenshot({ path: `test-results/character-card-cover-${theme}.png`, fullPage: true })
  }
})
