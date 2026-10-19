// Reuse the existing deterministic scenarios with native Gemini wire messages.
// Production tools, journals and product flows remain real.
const streams = new WeakMap()
const requests = []

export function geminiControl(url, response, writeJSON) {
  if (url.pathname !== '/control/gemini-requests') return false
  writeJSON(response, 200, requests)
  return true
}

export function geminiRequest(body, response, streaming) {
  requests.push(body)
  streams.set(response, { streaming, parts: [], usage: null })
  const messages = body.systemInstruction ? [{ role: 'system', content: body.systemInstruction.parts.map(part => part.text ?? '').join('') }] : []
  for (const content of body.contents ?? []) {
    const parts = []
    const calls = []
    for (const part of content.parts ?? []) {
      if (part.functionCall) {
        const call = part.functionCall
        if (part.thoughtSignature !== Buffer.from(`signature-${call.id}`).toString('base64')) throw new Error(`Missing native signature for ${call.id}`)
        calls.push({ id: call.id, type: 'function', function: { name: call.name, arguments: JSON.stringify(call.args ?? {}) } })
      } else if (part.functionResponse) {
        const result = part.functionResponse
        messages.push({ role: 'tool', tool_call_id: result.id, content: result.response.output ?? result.response.error ?? '' })
        if (result.parts?.length) messages.push({ role: 'user', content: [
          { type: 'text', text: `Image from tool call "${result.id}"` },
          ...result.parts.map(image => ({ type: 'image_url', image_url: { url: `data:${image.inlineData.mimeType};base64,${image.inlineData.data}` } })),
        ] })
      } else if (part.inlineData) {
        parts.push({ type: 'image_url', image_url: { url: `data:${part.inlineData.mimeType};base64,${part.inlineData.data}` } })
      } else if (part.text && !part.thought) parts.push({ type: 'text', text: part.text })
    }
    if (parts.length || calls.length) messages.push({ role: content.role === 'model' ? 'assistant' : 'user', content: parts, tool_calls: calls })
  }
  return { messages, stream: streaming, tools: (body.tools ?? []).flatMap(tool => (tool.functionDeclarations ?? []).map(declaration => ({ type: 'function', function: { ...declaration, parameters: declaration.parametersJsonSchema } }))) }
}

export function writeGeminiFrame(response, frame) {
  const stream = streams.get(response)
  if (!stream) return false
  const delta = frame.choices?.[0]?.delta
  const parts = []
  if (delta?.content) parts.push({ text: delta.content })
  for (const call of delta?.tool_calls ?? []) parts.push({
    functionCall: { id: call.id, name: call.function.name, args: JSON.parse(call.function.arguments) },
    thoughtSignature: Buffer.from(`signature-${call.id}`).toString('base64'),
  })
  stream.parts.push(...parts)
  if (parts.length) response.write(`data: ${JSON.stringify({ candidates: [{ index: 0, content: { role: 'model', parts } }] })}\n\n`)
  if (frame.usage) stream.usage = { promptTokenCount: frame.usage.prompt_tokens, candidatesTokenCount: frame.usage.completion_tokens, totalTokenCount: frame.usage.total_tokens }
  return true
}

export function finishGemini(response) {
  const stream = streams.get(response)
  if (!stream) return false
  response.write(`data: ${JSON.stringify({ candidates: [{ index: 0, finishReason: 'STOP' }], usageMetadata: stream.usage })}\n\n`)
  response.end()
  return true
}

export function writeGeminiGenerated(response, content, writeJSON) {
  if (!streams.has(response)) return false
  writeJSON(response, 200, { candidates: [{ index: 0, content: { role: 'model', parts: [{ text: content }] }, finishReason: 'STOP' }], usageMetadata: { promptTokenCount: 10, candidatesTokenCount: 10, totalTokenCount: 20 } })
  return true
}
