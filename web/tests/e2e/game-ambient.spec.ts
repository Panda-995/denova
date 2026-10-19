import { createServer } from 'node:http'
import { expect, test } from '../support/fixtures'
import { createAndOpenBook, createStartedStory } from '../support/api'
import { submitAgentChatMessage } from '../support/agent-chat'
import type { LoreItem } from '../../src/lib/api'
import type { Snapshot } from '../../src/features/interactive/types'

// Silent but valid media exercises browser decoding without speakers, a paid
// provider, or user files. A long loop makes accidental restarts observable.
function wave() {
  const data = Buffer.alloc(44 + 8000 * 2 * 30)
  data.write('RIFF'); data.writeUInt32LE(data.length - 8, 4)
  data.write('WAVEfmt ', 8); data.writeUInt32LE(16, 16)
  data.writeUInt16LE(1, 20); data.writeUInt16LE(1, 22)
  data.writeUInt32LE(8000, 24); data.writeUInt32LE(16000, 28)
  data.writeUInt16LE(2, 32); data.writeUInt16LE(16, 34)
  data.write('data', 36); data.writeUInt32LE(data.length - 44, 40)
  return data
}

type AudioWindow = Window & { sceneAudio: HTMLAudioElement[]; rejectScenePlayback?: () => void }

for (const theme of ['dark', 'light']) {
  test(`scene ambience persists, follows turns and yields audio focus in ${theme}`, async ({ page, request, browserDiagnostics }, testInfo) => {
    const frame = Buffer.alloc(417)
    frame.set([0xff, 0xfb, 0x90, 0xc0])
    const mp3 = Buffer.concat(Array.from({ length: 180 }, () => frame))
    const provider = createServer((_req, res) => {
      res.writeHead(200, { 'Content-Type': 'audio/mpeg' })
      res.end(mp3)
    })
    await new Promise<void>(resolve => provider.listen(0, '127.0.0.1', resolve))
    const address = provider.address()
    if (!address || typeof address === 'string') throw new Error('Speech fixture did not bind')
    try {
      const settings = await (await request.get('/api/settings')).json()
      const configured = await request.patch('/api/settings', { data: {
        layer: 'user', base_revision: settings.revisions.user,
        changes: { theme, language: 'zh-CN', speech: {
          endpoint: `http://127.0.0.1:${address.port}/audio/speech`, model: 'fixture', voice: 'fixture',
        } },
      } })
      expect(configured.ok(), await configured.text()).toBe(true)
      const book = await createAndOpenBook(request, `Scene audio ${theme}`)
      const story = await createStartedStory(request, `Ambient ${theme}`)
      const readSnapshot = async (branch = 'main'): Promise<Snapshot> => {
        const response = await request.get(`/api/interactive/stories/${story.id}/snapshot?branch=${branch}`)
        expect(response.ok(), await response.text()).toBe(true)
        return response.json()
      }
      await page.addInitScript(() => {
        const tracking = window as unknown as AudioWindow
        tracking.sceneAudio = []
        const NativeAudio = window.Audio
        window.Audio = class extends NativeAudio {
          constructor(src?: string) { super(src); tracking.sceneAudio.push(this) }
        }
      })
      await page.goto('/')
      const sidebar = page.getByLabel('工作台侧边栏')
      await sidebar.getByRole('button', { name: '游戏', exact: true }).click()
      await page.getByRole('tab', { name: '控制', exact: true }).click()
      await expect(page.getByRole('switch', { name: '允许 Agent 切换环境音' })).not.toBeChecked()
      await page.getByRole('button', { name: '当前环境音', exact: true }).click()
      await expect(page.getByText('暂无匹配音频，请先在资料库中上传 MP3 或 WAV 素材。')).toBeVisible()
      await page.getByRole('dialog').getByRole('button', { name: '无环境音', exact: true }).click()

      const loreRoute = `/api/projects/${book.projectId}/book/lore/items`
      const created = await request.post(loreRoute, { data: { id: 'station', name: '旧车站', type: 'location', content: 'A station.', enabled: true } })
      expect(created.ok(), await created.text()).toBe(true)
      const tavernName = `tavern-${'Long scene ambience '.repeat(6)}.wav`
      for (const name of ['rain.wav', tavernName]) {
        const uploaded = await request.post(`${loreRoute}/station/materials/upload`, { multipart: { file: { name, mimeType: 'audio/wav', buffer: wave() } } })
        expect(uploaded.ok(), await uploaded.text()).toBe(true)
      }
      const items: LoreItem[] = (await (await request.get(loreRoute)).json()).items
      const [rain, tavern] = items.find(item => item.id === 'station')!.resolved_materials!
      const before = await readSnapshot()
      await page.getByRole('button', { name: '当前环境音', exact: true }).click()
      await page.getByRole('dialog').getByRole('button', { name: /rain.wav.*旧车站/ }).click()
      const player = page.getByTestId('story-ambient-playback')
      await expect(player).toContainText('rain.wav')
      await expect(player).toHaveAttribute('data-status', 'paused')
      const selected = await readSnapshot()
      expect(selected.current_turn?.narrative).toBe(before.current_turn?.narrative)
      expect(selected.state).toEqual(before.state)
      expect(selected.current_turn?.turn_result?.presentation?.ambient).toMatchObject({ asset_id: rain.id, path: rain.path })
      await page.evaluate(() => {
        const tracking = window as unknown as AudioWindow
        const audio = tracking.sceneAudio.at(-1)!
        const play = audio.play.bind(audio)
        let delayed = true
        audio.play = () => {
          if (!delayed) return play()
          delayed = false
          return new Promise<void>((_resolve, reject) => {
            tracking.rejectScenePlayback = () => reject(new DOMException('Old playback interrupted', 'AbortError'))
          })
        }
      })
      await player.getByRole('button', { name: '播放环境音' }).click()
      await expect(player).toHaveAttribute('data-status', 'loading')
      await player.getByRole('button', { name: '暂停环境音' }).click()
      await player.getByRole('button', { name: '播放环境音' }).click()
      await expect(player).toHaveAttribute('data-status', 'playing')
      await page.evaluate(async () => {
        (window as unknown as AudioWindow).rejectScenePlayback?.()
        await new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)))
      })
      await expect(player).toHaveAttribute('data-status', 'playing')
      await player.getByRole('button', { name: '静音环境音' }).click()
      await expect(player.getByRole('button', { name: '静音环境音' })).toHaveAttribute('aria-pressed', 'true')
      await player.getByRole('slider', { name: '环境音音量' }).press('End')
      expect(await page.evaluate(() => localStorage.getItem('denova:ambient-volume'))).toBe('1')
      expect((await readSnapshot()).current_turn).toEqual(selected.current_turn)
      await player.getByRole('button', { name: '静音环境音' }).click()

      const composer = page.getByPlaceholder(/你要做什么/)
      await submitAgentChatMessage(page, composer, 'Continue in the same scene.')
      await expect.poll(async () => (await readSnapshot()).turns.length).toBe(2)
      await expect(player).toHaveAttribute('data-status', 'playing')
      expect(await page.evaluate(() => (window as unknown as AudioWindow).sceneAudio.filter(audio => audio.src.includes('asset')).length)).toBe(1)

      await page.getByRole('switch', { name: '允许 Agent 切换环境音' }).click()
      await expect(page.getByRole('switch', { name: '允许 Agent 切换环境音' })).toBeEnabled()
      await submitAgentChatMessage(page, composer, `E2E_AMBIENT_SELECT:station:${tavern.id}`)
      await expect.poll(async () => (await readSnapshot()).current_turn?.turn_result?.presentation?.ambient?.asset_id).toBe(tavern.id)
      await expect(player).toContainText(tavernName)
      await expect(player).toHaveAttribute('data-status', 'playing')

      await page.getByRole('button', { name: '朗读正文', exact: true }).first().click()
      await expect(page.getByTestId('speech-playback')).toContainText('正在朗读')
      await expect(player).toHaveAttribute('data-status', 'paused')
      await page.getByTestId('speech-playback').getByRole('button', { name: '暂停朗读' }).click()
      await player.getByRole('button', { name: '播放环境音' }).click()
      await expect(player).toHaveAttribute('data-status', 'playing')
      await sidebar.getByRole('button', { name: '写作', exact: true }).click()
      await expect.poll(() => page.evaluate(() => (window as unknown as AudioWindow).sceneAudio.every(audio => audio.paused)), { timeout: 5000 }).toBe(true)
      await sidebar.getByRole('button', { name: '游戏', exact: true }).click()
      await expect(player).toHaveAttribute('data-status', 'paused')
      await page.reload()
      await expect(player).toContainText(tavernName)
      await expect(player).toHaveAttribute('data-status', 'paused')

      browserDiagnostics.allow(/console\.error: Failed to load resource:.*404.*\/files\/asset/)
      const missingAudio = (url: URL) => url.pathname.endsWith('/files/asset') && url.searchParams.get('path') === tavern.path
      await page.route(missingAudio, route => route.fulfill({ status: 404 }))
      await player.getByRole('button', { name: '播放环境音' }).click()
      await expect(player).toHaveAttribute('data-status', 'error')
      await page.unroute(missingAudio)
      await player.getByRole('button', { name: '播放环境音' }).click()
      await expect(player).toHaveAttribute('data-status', 'playing')

      // Create through the UI so reload restores the selected branch instead of
      // the previously remembered main branch.
      await page.getByRole('button', { name: '从此处创建分支', exact: true }).last().click()
      const branchDialog = page.getByRole('dialog')
      await branchDialog.getByLabel('剧情线名称').fill('Side scene')
      const branchCreated = page.waitForResponse(response => response.request().method() === 'POST'
        && new URL(response.url()).pathname === `/api/interactive/stories/${story.id}/branches`)
      await branchDialog.getByRole('button', { name: '创建并切换', exact: true }).click()
      const branchResponse = await branchCreated
      expect(branchResponse.ok(), await branchResponse.text()).toBe(true)
      const branch: { id: string } = await branchResponse.json()
      await expect(branchDialog).toBeHidden()
      await page.reload()
      await submitAgentChatMessage(page, page.getByPlaceholder(/你要做什么/), `E2E_AMBIENT_SELECT:station:${rain.id}`)
      await expect.poll(async () => (await readSnapshot(branch.id)).current_turn?.turn_result?.presentation?.ambient?.asset_id).toBe(rain.id)
      expect((await readSnapshot()).current_turn?.turn_result?.presentation?.ambient?.asset_id).toBe(tavern.id)
      await player.getByRole('button', { name: '播放环境音' }).click()
      await expect(player).toHaveAttribute('data-status', 'playing')
      await page.getByRole('tab', { name: '路线', exact: true }).click()
      await page.getByRole('button', { name: '切换到剧情线：主线', exact: true }).click()
      await expect(player).toContainText(tavernName)
      await expect(player).toHaveAttribute('data-status', 'paused')
      await page.getByRole('tab', { name: '控制', exact: true }).click()
      await page.getByRole('button', { name: '当前环境音', exact: true }).scrollIntoViewIfNeeded()
      await page.screenshot({ path: testInfo.outputPath(`ambient-${theme}-wide.png`) })
      await page.setViewportSize({ width: 390, height: 844 })
      await expect(player).toBeVisible()
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
      await page.screenshot({ path: testInfo.outputPath(`ambient-${theme}-narrow.png`) })
      if (theme === 'light') {
        const latest = await (await request.get('/api/settings')).json()
        const english = await request.patch('/api/settings', { data: { layer: 'user', base_revision: latest.revisions.user, changes: { language: 'en-US' } } })
        expect(english.ok(), await english.text()).toBe(true)
        await page.reload()
        await expect(player.getByRole('button', { name: 'Play ambience', exact: true })).toBeVisible()
        await page.screenshot({ path: testInfo.outputPath('ambient-english-narrow.png') })
      }
    } finally {
      provider.closeAllConnections()
      await new Promise<void>(resolve => provider.close(() => resolve()))
    }
  })
}
