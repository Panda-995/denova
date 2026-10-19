// Deterministic model responses only. Production uses the ordinary Agent loop.
export function comicEpisodeCompletion(body) {
  const lastUser = body.messages.findLastIndex(message => message.role === 'user')
  const text = JSON.stringify(body.messages[lastUser])
  const mode = ['START', 'CONTINUE', 'ADOPT'].find(mode => text.includes(`E2E_RUNTIME_COMIC_EPISODE_${mode}`))
  if (!mode) return null
  const messages = body.messages.slice(lastUser)
  const calls = new Map(messages.flatMap(message => (message.tool_calls ?? []).map(call => [call.id, call.function])))
  const results = messages.filter(message => message.role === 'tool').map(message => {
    const call = calls.get(message.tool_call_id)
    const content = typeof message.content === 'string' ? message.content : JSON.stringify(message.content)
    const structured = content.split('Structured result:\n').at(-1)
    let data
    try { data = JSON.parse(structured) } catch { data = { fixtureError: content } }
    return { name: call?.name, input: JSON.parse(call?.arguments || '{}'), data }
  })
  const result = (name, predicate = () => true) => results.findLast(item => item.name === name && predicate(item.input))
  const failed = results.find(item => item.data.fixtureError)
  if (failed) return { content: `Comic fixture tool failed: ${failed.data.fixtureError}` }
  const tool = (name, input, extra = {}) => ({ tool: name, input, id: `comic-${mode.toLowerCase()}-${results.length}`, ...extra })
  const inspect = 'plugin_comic_inspect', structure = 'plugin_comic_structure', render = 'plugin_comic_render'
  const todo = result('todo')
  if (!todo) return tool('todo', { action: 'read' })
  if (todo.input.action === 'read') return tool('todo', { action: 'replace', expected_revision: todo.data.revision, items: [
    { id: 'pages', text: 'Generate the three saved comic pages', status: mode === 'ADOPT' ? 'completed' : 'in_progress' },
    { id: 'adopt', text: 'Review and adopt the episode when requested', status: mode === 'ADOPT' ? 'in_progress' : 'pending' },
  ] })
  const root = result(inspect, input => !input.episodeId)
  if (!root) return tool(inspect, {})
  const episodeId = root.data.episodes[0].id
  if (mode === 'START' && !result(structure, input => input.kind === 'page')) return tool(structure, { kind: 'page', episodeId, count: 2 })
  const episode = result(inspect, input => !!input.episodeId)
  if (!episode) return tool(inspect, { episodeId })
  const state = episode.data
  if (mode === 'START' && !result(structure, input => input.kind === 'plan')) {
    const context = JSON.parse(body.messages[lastUser].content.match(/\{"pageId":[^\n]+\}/)[0])
    return tool(structure, { kind: 'plan', episodeId, expectedRevision: state.revision,
      references: [{ libraryId: 'hero', note: 'Character identity' }, ...context.references],
      pages: state.pages.map((page, index) => ({ pageId: page.id, expectedRevision: page.revision, prompt: `E2E_COMIC_PAGE E2E_COMIC_EPISODE_IMAGE_${index + 1} Three panels at a rainy station; preserve character identity and watercolor style.` })),
    })
  }
  const latest = results.at(-1)
  if (latest.name !== inspect) return tool(inspect, { episodeId })
  if (mode === 'ADOPT') {
    if (!result('plugin_comic_adopt')) return tool('plugin_comic_adopt', { items: state.pages.map(page => ({ pageId: page.id, resultId: page.candidate.id, expectedCurrentId: page.expectedCurrentId })) })
    if (todo.data.items.find(item => item.id === 'adopt').status !== 'completed') return tool('todo', { action: 'update', expected_revision: todo.data.revision, mutations: [{ id: 'adopt', status: 'completed' }] })
    return { content: `整话已采用：${state.progress.adopted}/3。` }
  }
  const next = state.pages.find(page => !page.candidate)
  if (next) return tool(render, { pageId: next.id, ...(next.needsRecovery ? { action: 'recover', commandId: next.commandId } : {}) },
    mode === 'START' && state.progress.generated === 1 ? { hold: 'E2E_COMIC_EPISODE_HOLD' } : {})
  if (todo.data.items.find(item => item.id === 'pages').status !== 'completed') return tool('todo', { action: 'update', expected_revision: todo.data.revision, mutations: [{ id: 'pages', status: 'completed' }] })
  return { content: '整话已生成：3/3，尚未采用。' }
}
