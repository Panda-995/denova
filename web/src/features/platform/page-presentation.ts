/** Optional display metadata from a page frame. It never grants capabilities or
 * submits Agent input; suggestions only fill the host-owned composer. */
export interface PagePresentation {
  title?: string
  contextLabel?: string
  referenceCount?: number
  suggestions?: { label: string; prompt: string }[]
  actions?: { id: string; label: string }[]
}

export function parsePagePresentation(value: unknown): PagePresentation | undefined {
  if (!value || typeof value !== 'object' || new TextEncoder().encode(JSON.stringify(value)).byteLength > 8192) return
  const data = value as PagePresentation
  const short = (text: unknown) => typeof text === 'string' && text.length <= 200
  if (data.title !== undefined && !short(data.title)) return
  if (data.contextLabel !== undefined && !short(data.contextLabel)) return
  if (
    data.referenceCount !== undefined &&
    (!Number.isInteger(data.referenceCount) || data.referenceCount < 0 || data.referenceCount > 100)
  )
    return
  if (
    data.actions !== undefined &&
    (!Array.isArray(data.actions) ||
      data.actions.length > 4 ||
      data.actions.some((action) => !action || !short(action.id) || !short(action.label)))
  )
    return
  if (
    data.suggestions !== undefined &&
    (!Array.isArray(data.suggestions) ||
      data.suggestions.length > 4 ||
      data.suggestions.some(
        (item) => !item || !short(item.label) || typeof item.prompt !== 'string' || item.prompt.length > 2000,
      ))
  )
    return
  return data
}
