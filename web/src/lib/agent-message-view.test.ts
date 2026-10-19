import { describe, expect, it } from 'vitest'
import type { AgentUIMessage } from '@/lib/agent-ui'
import { agentViewToRenderMessage, buildAgentMessageViews, shareAgentMessageViews } from './agent-message-view'

describe('agentViewToRenderMessage', () => {
  it.each(['pending', 'answered', 'cancelled'])('restores %s tool approvals whose journal payload omits questions', (status) => {
    const ask: AgentUIMessage = {
      id: 'approval', role: 'assistant', metadata: { run_id: 'run' },
      parts: [{ type: 'data-agent-ask', data: {
        schema: 'ask.pending.v1', id: 'approval', kind: 'tool_approval', tool_call_id: 'write-call', agent_kind: 'ide', status,
        approval: { mode: 'write', tool_name: 'write', risk: 'high', rule_id: 'external_mutation' },
      } }],
    }
    const tool: AgentUIMessage = {
      id: 'tool', role: 'assistant', metadata: { run_id: 'run' },
      parts: [{ type: 'dynamic-tool', toolName: 'write', toolCallId: 'write-call', state: 'input-available', input: { path: 'draft.md' } }],
    }
    const views = buildAgentMessageViews([tool, ask])
    expect(views.map(view => view.kind)).toEqual(['tool'])
    expect(agentViewToRenderMessage(views[0])).toMatchObject({ ask: { id: 'approval', kind: 'tool_approval', status, questions: [] } })
    const [orphan] = buildAgentMessageViews([ask])
    expect(agentViewToRenderMessage(orphan)).toMatchObject({ role: 'ask', ask: { id: 'approval', kind: 'tool_approval', status, questions: [] } })
  })

  const chapter = 'Chapter body. '.repeat(500)
  const history: AgentUIMessage[] = [{
    id: 'write-reply', role: 'assistant', metadata: { run_id: 'run-a' },
    parts: [{
      type: 'tool-write', toolCallId: 'call-a', state: 'output-available',
      input: { path: 'chapters/001.md', content: chapter },
      output: JSON.stringify({ status: 'success', path: 'chapters/001.md' }),
    } as AgentUIMessage['parts'][number]],
  }]

  it('reuses the converted message when a settled row remounts', () => {
    const [view] = buildAgentMessageViews(history)
    const first = agentViewToRenderMessage(view)

    expect(first).toMatchObject({ role: 'tool_call', name: 'write', args: expect.stringContaining('chapters/001.md') })
    expect(agentViewToRenderMessage(buildAgentMessageViews(history)[0])).toBe(first)
  })

  it('builds a separate settled copy for forced completion', () => {
    const [view] = buildAgentMessageViews([{ ...history[0], id: 'streaming-reply', parts: [{ type: 'text', text: 'Drafting', state: 'streaming' }] }])

    expect(agentViewToRenderMessage(view)).toMatchObject({ role: 'assistant', streaming: true })
    expect(agentViewToRenderMessage(view, { forceDone: true })).toMatchObject({ role: 'assistant', streaming: false })
    expect(agentViewToRenderMessage(view)).toMatchObject({ streaming: true })
  })

  it('shares completed parts across cloned cumulative snapshots without hiding tool updates', () => {
    const messages: AgentUIMessage[] = [{ ...history[0], parts: [...history[0].parts, { type: 'text', text: 'Drafting', state: 'streaming' }] }]
    const previous = buildAgentMessageViews(messages)
    const snapshot = structuredClone(messages)
    snapshot[0].parts[1] = { type: 'text', text: 'Drafting the next chapter', state: 'streaming' }
    const next = shareAgentMessageViews(previous, buildAgentMessageViews(snapshot))
    expect(next[0]).toBe(previous[0])
    expect(agentViewToRenderMessage(next[0])).toBe(agentViewToRenderMessage(previous[0]))
    expect(next[1]).not.toBe(previous[1])
    const completed = structuredClone(snapshot)
    completed[0].parts[0] = { ...completed[0].parts[0], output: 'Updated result' } as AgentUIMessage['parts'][number]
    const changed = shareAgentMessageViews(next, buildAgentMessageViews(completed))
    expect(changed[0]).not.toBe(next[0])
    expect(agentViewToRenderMessage(changed[0])).toMatchObject({ result: 'Updated result' })
    expect(changed[1]).toBe(next[1])
  })
})
