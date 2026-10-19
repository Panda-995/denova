import { createContext, useContext, type ReactNode } from 'react'

interface BookCreationLifecycle {
  /** Flush retained workspace drafts before the API creates and selects a book. */
  beforeCreate: () => Promise<boolean>
  /** Synchronize the selected book and bookshelf as soon as creation succeeds. */
  onCreated: (workspace: string) => void | Promise<void>
}

const BookCreationContext = createContext<BookCreationLifecycle | null>(null)

export function BookCreationProvider({ value, children }: { value: BookCreationLifecycle; children: ReactNode }) {
  return <BookCreationContext.Provider value={value}>{children}</BookCreationContext.Provider>
}

export function useBookCreation() {
  const lifecycle = useContext(BookCreationContext)
  if (!lifecycle) throw new Error('Book creation requires the workspace lifecycle provider')
  return lifecycle
}
