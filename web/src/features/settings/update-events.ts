export const UPDATE_CHECK_RESULT_EVENT = 'nova:update-check-result'

export function notifyUpdateCheckResult<T>(result: T) {
  if (typeof window === 'undefined') return
  window.dispatchEvent(new CustomEvent<T>(UPDATE_CHECK_RESULT_EVENT, { detail: result }))
}
