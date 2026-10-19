import { createContext, useCallback, useContext, useRef, useState, type ReactNode, type SetStateAction } from 'react'
import { createStore, useStore } from 'zustand'

type Entries = Record<string, Record<string, unknown>>
const createMessageState = () => createStore<Entries>(() => ({}))
const StateContext = createContext<ReturnType<typeof createMessageState> | null>(null)
const RowContext = createContext('')

/** Ephemeral transcript state outlives virtual rows, and resets with the conversation. */
export function VirtualizedMessageState({ children }: { children: ReactNode }) {
  const [store] = useState(createMessageState)
  return <StateContext.Provider value={store}>{children}</StateContext.Provider>
}

export const VirtualizedMessageRow = RowContext.Provider

/** Standalone cards retain ordinary local state. Each slot belongs to one row. */
export function useVirtualizedMessageState<T>(slot: string, initial: T | (() => T)): [T, (value: SetStateAction<T>) => void] {
  const shared = useContext(StateContext)
  const row = useContext(RowContext)
  const [fallback] = useState(createMessageState)
  const store = shared && row ? shared : fallback
  const initialRef = useRef<{ slot: string; value: T } | null>(null)
  if (!initialRef.current || initialRef.current.slot !== slot) initialRef.current = { slot, value: typeof initial === 'function' ? (initial as () => T)() : initial }
  const initialValue = initialRef.current.value
  const value = useStore(store, state => (state[row]?.[slot] as T | undefined) ?? initialValue)
  const setValue = useCallback((update: SetStateAction<T>) => {
    store.setState(state => {
      const previous = (state[row]?.[slot] as T | undefined) ?? initialValue
      const next = typeof update === 'function' ? (update as (value: T) => T)(previous) : update
      if (Object.is(previous, next)) return state
      return { ...state, [row]: { ...state[row], [slot]: next } }
    }, true)
  }, [initialValue, row, slot, store])
  return [value, setValue]
}
