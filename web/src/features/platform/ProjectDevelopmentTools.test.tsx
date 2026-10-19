import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { ProjectDevelopmentTools } from './ProjectDevelopmentTools'
import { management, type RuntimeSnapshot } from './api'

vi.mock('./api', () => ({ management: vi.fn().mockResolvedValue([]), platformError: () => 'failed' }))
vi.mock('react-i18next', () => ({ useTranslation: () => ({ t: (key: string) => key, i18n: { language: 'en-US' } }) }))
vi.mock('./development-context', () => ({
  useProjectDevelopment: () => ({ sources: [], source: { developmentId: 'source', projectId: 'project', relativePath: '.', kind: 'game', manifest: { id: 'test.game' } } }),
  useDevelopmentContext: { getState: () => ({ recordFeedback: vi.fn() }) },
}))
vi.mock('./DevelopmentPreviewDialog', () => ({ DevelopmentPreviewDialog: ({ onPlay, onClose }: { onPlay: (runtime: RuntimeSnapshot) => void; onClose: () => void }) => <button onClick={() => { onPlay({ id: 'preview', viewUrl: 'http://127.0.0.1:18090/view', context: { environment: 'preview', scope: { projectId: 'project' }, source: { package: { id: 'test.game' } } } } as RuntimeSnapshot); onClose() }}>Open preview</button> }))
vi.mock('./BuildTerminal', () => ({ BuildTerminal: () => null }))
vi.mock('./SourceManifestEditor', () => ({ SourceManifestEditor: () => null }))
vi.mock('./extension-navigation', () => ({ openInstalledExtension: vi.fn() }))

afterEach(() => { cleanup(); vi.clearAllMocks() })

describe('workbench source actions', () => {
  it('does not check a package until open editors have saved', async () => {
    const beforeAction = vi.fn().mockResolvedValue(false)
    render(<QueryClientProvider client={new QueryClient()}><ProjectDevelopmentTools projectId="project" visible refreshSignal={0} beforeAction={beforeAction} onOpenFile={vi.fn()} /></QueryClientProvider>)
    fireEvent.click(screen.getByRole('button', { name: 'platform.preview' }))
    await waitFor(() => expect(beforeAction).toHaveBeenCalled())
    expect(vi.mocked(management).mock.calls.some(([path]) => path.endsWith('/check'))).toBe(false)
  })

  it('can stop a preview even when an editor cannot save', async () => {
    const beforeAction = vi.fn().mockResolvedValue(true)
    vi.mocked(management).mockImplementation(async path => path.endsWith('/check') ? { candidateId: 'candidate' } as never : [] as never)
    render(<QueryClientProvider client={new QueryClient()}><ProjectDevelopmentTools projectId="project" visible refreshSignal={0} beforeAction={beforeAction} onOpenFile={vi.fn()} /></QueryClientProvider>)
    fireEvent.click(screen.getByRole('button', { name: 'platform.preview' }))
    fireEvent.click(await screen.findByRole('button', { name: 'Open preview' }))
    await screen.findByTitle('platform.gameFrame')
    beforeAction.mockClear().mockResolvedValue(false)
    fireEvent.keyDown(document, { key: 'Escape' })
    await waitFor(() => expect(management).toHaveBeenCalledWith('/runtimes/preview/stop', 'POST', {}))
    expect(beforeAction).not.toHaveBeenCalled()
  })

  it('honors the frame save guard when the preview dialog is dismissed', async () => {
    vi.mocked(management).mockImplementation(async path => path.endsWith('/check') ? { candidateId: 'candidate' } as never : [] as never)
    render(<QueryClientProvider client={new QueryClient()}><ProjectDevelopmentTools projectId="project" visible refreshSignal={0} beforeAction={async () => true} onOpenFile={vi.fn()} /></QueryClientProvider>)
    fireEvent.click(screen.getByRole('button', { name: 'platform.preview' }))
    fireEvent.click(await screen.findByRole('button', { name: 'Open preview' }))
    const frame = await screen.findByTitle('platform.gameFrame') as HTMLIFrameElement
    const post = vi.spyOn(frame.contentWindow!, 'postMessage')
    fireEvent(window, new MessageEvent('message', { source: frame.contentWindow, origin: 'http://127.0.0.1:18090', data: { type: 'denova:ready', nonce: 'init', exitGuard: true } }))
    fireEvent.keyDown(document, { key: 'Escape' })
    await waitFor(() => expect(post.mock.calls.some(([value]) => value.type === 'denova:prepare-exit')).toBe(true))
    expect(management).not.toHaveBeenCalledWith('/runtimes/preview/stop', 'POST', {})
    const prepare = post.mock.calls.find(([value]) => value.type === 'denova:prepare-exit')![0]
    fireEvent(window, new MessageEvent('message', { source: frame.contentWindow, origin: 'http://127.0.0.1:18090', data: { type: 'denova:exit-ready', requestId: prepare.requestId, ok: false } }))
    await screen.findByText('platform.exitNotReady')
    expect(screen.getByTitle('platform.gameFrame')).toBe(frame)
    expect(management).not.toHaveBeenCalledWith('/runtimes/preview/stop', 'POST', {})
  })
})
