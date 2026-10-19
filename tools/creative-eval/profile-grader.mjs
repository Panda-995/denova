import { randomUUID } from 'node:crypto'

// Resolve an explicitly selected Denova profile for grading without persisting
// its credential in promptfoo's configuration, reports, or local history.
export function profileGrader(settings, profileID) {
  const profile = settings.model_profiles?.find(item => item.id === profileID && item.enabled !== false)
  if (!profile?.model) throw new Error(`Grading profile is unavailable: ${profileID}`)
  const endpoint = settings.model_endpoints?.find(item => item.id === profile.endpoint_id && item.enabled !== false)
  if (!endpoint?.base_url) throw new Error(`Grading endpoint is unavailable for profile: ${profileID}`)
  const protocol = {
    'openai-chat-completions': 'openai:chat',
    'openai-responses': 'openai:responses',
  }[endpoint.protocol]
  if (!protocol || Object.keys(endpoint.headers || {}).length || Object.keys(endpoint.protocol_options || {}).length) {
    throw new Error(`Grading profile ${profileID} requires an explicit promptfoo grader configuration`)
  }
  const keyVariable = `DENOVA_EVAL_GRADER_${randomUUID().replaceAll('-', '_')}`
  process.env[keyVariable] = endpoint.api_key || 'not-required'
  return {
    provider: { id: `${protocol}:${profile.model}`, config: {
      apiBaseUrl: endpoint.base_url, apiKeyEnvar: keyVariable,
    } },
    release() { delete process.env[keyVariable] },
  }
}
