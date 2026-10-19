import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import type { Snapshot, StoryHistoryPage, TurnEvent } from '../../types'
import { getInteractiveExecutionDetails } from '../../api'
import {
  STORY_HISTORY_CACHE_MAX_BYTES,
  STORY_HISTORY_CACHE_MAX_TURNS,
  boundStoryTurns,
  createStoryHistoryWindow,
  prependStoryHistoryPage,
  projectStoryHistorySnapshot,
  reconcileStoryHistoryWindow,
} from './story-history-window'

export function useStoryHistoryWindow(stageKey: string, snapshot: Snapshot | null) {
  const [historyWindow, setHistoryWindow] = useState(() => createStoryHistoryWindow(stageKey, snapshot))
  const [executionDetails, setExecutionDetails] = useState<TurnEvent[]>([])
  const pendingDetails = useRef(new Map<string, Promise<void>>())

  useEffect(() => {
    setHistoryWindow((current) => reconcileStoryHistoryWindow(current, stageKey, snapshot))
  }, [snapshot, stageKey])

  const historySnapshot = useMemo(
    () => projectStoryHistorySnapshot(snapshot, historyWindow, stageKey),
    [historyWindow, snapshot, stageKey],
  )
  const detailsByCursor = useMemo(() => new Map(executionDetails.map(turn => [turn.execution_cursor, turn])), [executionDetails])
  const displaySnapshot = useMemo(() => historySnapshot && ({
    ...historySnapshot,
    turns: historySnapshot.turns.map(turn => {
      const details = turn.execution_cursor && detailsByCursor.get(turn.execution_cursor)
      return details ? { ...turn, thinking: details.thinking, display_events: details.display_events, execution_cursor: undefined } : turn
    }),
  }), [detailsByCursor, historySnapshot])

  // Details share the window's byte budget and lifetime. New summary cursors
  // invalidate old evidence after edits or additional display events.
  useEffect(() => {
    const retained = new Set(historySnapshot?.turns.map(turn => turn.execution_cursor))
    setExecutionDetails(current => {
      const entries = boundStoryTurns(current.filter(turn => retained.has(turn.execution_cursor)), 'latest', {
        maxTurns: STORY_HISTORY_CACHE_MAX_TURNS,
        maxBytes: Math.max(0, STORY_HISTORY_CACHE_MAX_BYTES - historyWindow.approximateBytes),
      }).turns
      return entries.length === current.length ? current : entries
    })
  }, [historySnapshot, historyWindow.approximateBytes])

  const loadExecutionDetails = useCallback(async (turnId: string) => {
    const turn = historySnapshot?.turns.find(turn => turn.id === turnId)
    const cursor = turn?.execution_cursor
    if (!cursor || !turn || !historySnapshot || detailsByCursor.has(cursor)) return
    const pending = pendingDetails.current.get(cursor)
    if (pending) return pending
    const request = getInteractiveExecutionDetails(historySnapshot.story_id, historySnapshot.branch_id, cursor)
      .then(details => {
        setExecutionDetails(current => boundStoryTurns([
          ...current,
          { ...turn, thinking: details.thinking, display_events: details.display_events },
        ], 'latest', {
          maxTurns: STORY_HISTORY_CACHE_MAX_TURNS,
          maxBytes: Math.max(0, STORY_HISTORY_CACHE_MAX_BYTES - historyWindow.approximateBytes),
        }).turns)
      })
      .finally(() => { pendingDetails.current.delete(cursor) })
    pendingDetails.current.set(cursor, request)
    return request
  }, [detailsByCursor, historySnapshot, historyWindow.approximateBytes])
  const prependPage = useCallback((page: StoryHistoryPage) => {
    setHistoryWindow((current) => prependStoryHistoryPage(current, stageKey, page))
  }, [stageKey])
  const resetToLatest = useCallback(() => {
    setHistoryWindow(createStoryHistoryWindow(stageKey, snapshot))
  }, [snapshot, stageKey])

  return { displaySnapshot, historyWindow, prependPage, resetToLatest, loadExecutionDetails }
}
