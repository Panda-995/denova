import { describe, expect, it } from 'vitest'
import { normalizeAgentUIMessages, type AgentUIMessage } from './agent-ui'

describe('Agent history and replay reconciliation', () => {
  it.each(['text', 'reasoning'] as const)('keeps durable %s while replay catches up and then extends it', (type) => {
    const history: AgentUIMessage = { id: 'history', role: 'assistant', metadata: { run_id: 'run', display_segment_id: 'segment' }, parts: [{ type, text: 'Complete answer.', state: 'done' }] }
    const replay = (text: string, state: 'streaming' | 'done' = 'streaming'): AgentUIMessage => ({ id: 'replay', role: 'assistant', parts: [{ type, text, state, providerMetadata: { agent: { run_id: 'run', display_segment_id: 'segment' } } }] })
    for (const prefix of ['', 'Comp', 'Complete answer.']) {
      expect(normalizeAgentUIMessages([history, replay(prefix)])[0].parts[0]).toMatchObject({ text: 'Complete answer.', state: 'streaming' })
    }
    expect(normalizeAgentUIMessages([history, replay('Complete answer. Continued.', 'done')])[0].parts[0]).toMatchObject({ text: 'Complete answer. Continued.', state: 'done' })
  })

  it('deduplicates legacy reasoning when a growing replay reaches its matching prefix', () => {
    const text = 'This reasoning has more than twenty four characters.'
    const history: AgentUIMessage = { id: 'history', role: 'assistant', metadata: { run_id: 'run' }, parts: [{ type: 'reasoning', text }] }
    const replay = (value: string): AgentUIMessage => ({ id: 'replay', role: 'assistant', parts: [{ type: 'reasoning', text: value, state: 'streaming', providerMetadata: { agent: { run_id: 'run', display_segment_id: 'segment' } } }] })
    expect(normalizeAgentUIMessages([history, replay('This')])).toHaveLength(2)
    expect(normalizeAgentUIMessages([history, replay(text)])).toHaveLength(1)
  })

  it('still retracts a failed segment after retaining its longer history during replay', () => {
    const metadata = { run_id: 'run', display_segment_id: 'failed' }
    const history: AgentUIMessage = { id: 'history', role: 'assistant', metadata, parts: [{ type: 'text', text: 'Failed preview.' }] }
    const replay: AgentUIMessage = { id: 'replay', role: 'assistant', parts: [{ type: 'text', text: 'Failed', providerMetadata: { agent: metadata } }] }
    const retry: AgentUIMessage = { id: 'retry', role: 'assistant', parts: [{ type: 'data-agent-activity', data: { event: 'model_retry', discard_ids: ['failed'] } }] }
    expect(normalizeAgentUIMessages([history, replay, retry])).toEqual([retry])
  })

  it('preserves a conflicting stable segment and reports its identity for recovery unless explicitly discarded', () => {
    const metadata = { run_id: 'run', display_segment_id: 'segment' }
    const history: AgentUIMessage = { id: 'history', role: 'assistant', metadata, parts: [{ type: 'text', text: 'Known answer.' }] }
    const replay: AgentUIMessage = { id: 'replay', role: 'assistant', metadata, parts: [{ type: 'text', text: 'Different answer.' }] }
    const conflicts: string[] = []
    expect(normalizeAgentUIMessages([history, replay], identity => conflicts.push(identity))).toEqual([history])
    expect(conflicts).toHaveLength(1)
    expect(conflicts[0]).toContain('segment')

    const retry: AgentUIMessage = { id: 'retry', role: 'assistant', parts: [{ type: 'data-agent-activity', data: { event: 'model_retry', discard_ids: ['segment'] } }] }
    expect(normalizeAgentUIMessages([history, replay, retry], identity => conflicts.push(identity))).toEqual([retry])
    expect(conflicts).toHaveLength(1)
  })

  it('keeps distinct legacy reasoning that only shares an opening', () => {
    const messages: AgentUIMessage[] = ['First conclusion.', 'Different conclusion.'].map((ending, i) => ({
      id: `message-${i}`, role: 'assistant', metadata: { run_id: 'run' },
      parts: [{ type: 'reasoning', text: `This opening has more than twenty four characters. ${ending}` }],
    }))
    expect(normalizeAgentUIMessages(messages)).toEqual(messages)
  })
})
