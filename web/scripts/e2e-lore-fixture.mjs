// Exercise the real configuration Agent, Lore discovery and image tool commit path.
export function loreQueryCompletion(body) {
  const messages = body.messages ?? []
  const userIndex = messages.findLastIndex(message => message.role === 'user' && JSON.stringify(message.content).includes('E2E_LORE_QUERY'))
  if (userIndex < 0) return null
  const tools = (body.tools ?? []).map(tool => tool.function?.name)
  if (!tools.includes('query_lore_items') || tools.includes('read_lore_items') || tools.includes('list_lore_items')) return { content: 'Unexpected lore tool catalog.' }
  const history = messages.slice(userIndex + 1)
  const calls = [
    { names: ['Hero'] },
    { ids: ['harbor', 'absent', 'hero', 'hidden'], detail: 'full', limit: 1 },
    { ids: ['harbor', 'absent', 'hero', 'hidden'], detail: 'full', limit: 1, offset: 1 },
    { keywords: ['Hero'], detail: 'full' },
  ]
  const results = []
  for (let index = 0; index < calls.length; index++) {
    const id = `lore-query-${index}`
    const result = history.find(message => message.role === 'tool' && message.tool_call_id === id)
    if (!result) return { tool: 'query_lore_items', arguments: JSON.stringify(calls[index]), id }
    results.push(typeof result.content === 'string' ? result.content : result.content.map(part => part.text ?? '').join(''))
  }
  const [brief, first, second, search] = results
  if (!brief.includes('E2E_QUERY_HERO_BRIEF') || brief.includes('E2E_QUERY_HERO_BODY') || brief.includes('E2E_QUERY_HARBOR_BODY')
    || !brief.includes('material_count: 0') || !brief.includes('cover_asset_id: ""')
    || !first.includes('E2E_QUERY_HARBOR_BODY') || first.includes('E2E_QUERY_HERO_BODY') || !first.includes('next_offset: 1')
    || !first.includes('missing_ids: ["absent","hidden"]')
    || !second.includes('E2E_QUERY_HERO_BODY') || !second.includes('next_offset: null')
    || !search.includes('E2E_QUERY_HERO_BODY') || !search.includes('E2E_QUERY_HARBOR_BODY')
    || results.some(result => result.includes('E2E_QUERY_HIDDEN_BODY'))) return { content: 'Lore query contract failed.' }
  return { content: 'E2E lore query verified.' }
}

export function loreCompletion(body) {
  const messages = body.messages ?? []
  const userIndex = messages.findLastIndex(message => message.role === 'user' && JSON.stringify(message.content).includes('E2E_LORE_BATCH'))
  if (userIndex < 0) return null
  const instruction = typeof messages[userIndex].content === 'string'
    ? messages[userIndex].content
    : JSON.stringify(messages[userIndex].content).replaceAll('\\n', '\n').replaceAll('\\"', '"')
  const ids = JSON.parse(instruction.match(/Exact lore item IDs: (\[[^\n]+\])/)[1])
  const missingOnly = instruction.includes('Only fill missing covers.')
  const history = messages.slice(userIndex + 1)
  const calls = history.flatMap(message => message.tool_calls ?? [])
  const call = (tool, args, id) => ({ tool, arguments: JSON.stringify(args), id })
  const queryCalls = calls.filter(entry => entry.function.name === 'query_lore_items')
  const resultText = id => {
    const content = history.find(message => message.role === 'tool' && message.tool_call_id === id)?.content
    return typeof content === 'string' ? content : (content ?? []).map(part => part.text ?? '').join('')
  }
  if (!queryCalls.length) return call('query_lore_items', { ids, detail: 'full' }, `query-${userIndex}-0`)
  const lastPage = resultText(queryCalls.at(-1).id)
  const next = lastPage.match(/^next_offset:\s*(\d+)$/m)?.[1]
  if (next) return call('query_lore_items', { ids, detail: 'full', offset: Number(next) }, `query-${userIndex}-${next}`)
  const pages = queryCalls.map(entry => resultText(entry.id)).join('\n')
  for (const id of ids) {
    const entry = pages.split(/^## /m).find(chunk => chunk.includes(`ID: ${id}\n`))
    if (!entry) return { content: `Missing selected item: ${id}` }
    if (missingOnly && /^cover_asset_id:\s*"[^"]+"/m.test(entry)) continue
    const imageID = `image-${userIndex}-${id}`
    if (!calls.some(entry => entry.id === imageID)) {
      return call('generate_image', {
        purpose: 'lore_item', lore_item_id: id, prompt: `A watercolor portrait for ${id}, soft daylight, no text.`,
        ...(missingOnly ? { lore_cover: 'if_missing' } : {}),
      }, imageID)
    }
  }
  return { content: 'E2E Lore batch completed.' }
}

// Only model decisions are deterministic; revisions, commits, receipts and
// UI invalidation travel through the production configuration workflow.
export function loreIndexCompletion(body) {
  const messages = body.messages ?? []
  const userIndex = messages.findLastIndex(message => message.role === 'user' && JSON.stringify(message.content).includes('E2E_LORE_INDEX_CONFIG'))
  if (userIndex < 0) return null
  const instruction = JSON.stringify(messages[userIndex].content)
  if (!instruction.includes('lore_index') || !instruction.includes('/configuration')) return { content: 'Index page context is missing.' }
  const history = messages.slice(userIndex + 1)
  const result = id => history.find(message => message.role === 'tool' && message.tool_call_id === id)
  const value = id => {
    const content = result(id)?.content
    try { return JSON.parse(typeof content === 'string' ? content : content?.map(part => part.text ?? '').join('')) } catch { return {} }
  }
  const call = (tool, args, id) => ({ tool, arguments: JSON.stringify(args), id })
  if (!result('index-describe')) return call('config_read', { operation: 'describe' }, 'index-describe')
  if (!JSON.stringify(value('index-describe')).includes('lore_index_membership')) return { content: 'Index resources are unavailable.' }
  if (!result('index-reference')) return call('read', { path: 'skill://configuration/references/lore-index.md' }, 'index-reference')
  if (!JSON.stringify(result('index-reference').content).includes('index_memberships')) return { content: 'Index Skill reference is unavailable.' }
  if (!result('index-get')) return call('config_read', { operation: 'get', resource: 'lore_index', ids: ['index'] }, 'index-get')
  const index = value('index-get').items?.[0]
  if (!index) return { content: 'Index read failed.' }
  if (!result('index-apply')) return call('config_apply', {
    operation: 'update', resource: 'lore_index', id: 'index', revision: index.revision,
    value: { ...index.guide, intro_markdown: 'E2E_CONFIG_READING_GUIDE', groups: [...index.guide.groups, {
      id: 'port-cast', name: '港口人物', purpose: 'E2E_CONFIG_PURPOSE', body_markdown: 'E2E_CONFIG_NOTES', default_detail: 'full',
    }], automatic_details: { ...index.guide.automatic_details, 'auto:location': 'brief' } },
  }, 'index-apply')
  if (!result('member-get')) return call('config_read', { operation: 'get', resource: 'lore_index_membership', ids: ['captain'] }, 'member-get')
  const member = value('member-get').items?.[0]
  if (!member) return { content: 'Membership read failed.' }
  if (!result('member-apply')) return call('config_apply', {
    operation: 'update', resource: 'lore_index_membership', id: member.id, revision: member.revision,
    value: { index_memberships: [...member.index_memberships, { group_id: 'port-cast', detail: 'inherit' }] },
  }, 'member-apply')
  if (!result('index-verify')) return call('config_read', { operation: 'get', resource: 'lore_index', ids: ['index'] }, 'index-verify')
  if (!result('member-verify')) return call('config_read', { operation: 'get', resource: 'lore_index_membership', ids: ['captain'] }, 'member-verify')
  if (value('index-verify').items?.[0]?.guide?.groups?.some(group => group.id === 'port-cast')
    && value('member-verify').items?.[0]?.index_memberships?.some(member => member.group_id === 'port-cast')) {
    return { content: 'E2E index configuration verified.' }
  }
  return { content: 'Index configuration verification failed.' }
}
