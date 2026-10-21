/** Search names by substring or adjacent word prefixes; ties retain the group's original order. */
export function searchInputCommands<T>(commands: readonly T[], query: string | null, name: (command: T) => string): T[] {
  if (query === null) return []
  const normalizedQuery = query.toLowerCase()
  return commands
    .map((command) => ({ command, position: matchPosition(name(command).replace(/^\//, '').toLowerCase(), normalizedQuery) }))
    .filter(({ position }) => Number.isFinite(position))
    .sort((left, right) => left.position - right.position)
    .map(({ command }) => command)
}

function matchPosition(name: string, query: string): number {
  const substringPosition = name.indexOf(query)
  let bestPosition = substringPosition < 0 ? Infinity : substringPosition
  if (bestPosition === 0) return 0

  // Each word consumes a nonempty prefix. Keep only the earliest start for each
  // consumed query length to avoid enumerating ambiguous splits.
  let partialMatches = new Map<number, number>()
  for (const word of name.matchAll(/[\p{L}\p{N}\p{M}]+/gu)) {
    partialMatches.set(0, word.index)
    const next = new Map<number, number>()
    for (const [offset, start] of partialMatches) {
      for (let length = 1; length <= word[0].length; length++) {
        const consumed = offset + length
        if (word[0][length - 1] !== query[consumed - 1]) break
        if (consumed === query.length) {
          bestPosition = Math.min(bestPosition, start)
          break
        }
        next.set(consumed, Math.min(next.get(consumed) ?? Infinity, start))
      }
    }
    partialMatches = next
  }
  return bestPosition
}
