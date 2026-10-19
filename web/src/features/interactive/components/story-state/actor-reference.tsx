import { createContext, useContext, useMemo, type ReactNode } from 'react'
import { actorName, type ActorStateEntry } from './model'

const ActorNamesContext = createContext<ReadonlyMap<string, string>>(new Map())

/** Display references using the same snapshot as the Actor tabs, including archives. */
export function ActorReferenceProvider({ actors, children }: { actors: ActorStateEntry[]; children: ReactNode }) {
  const names = useMemo(() => new Map(actors.map(([id, actor]) => [id, actorName(id, actor)])), [actors])
  return <ActorNamesContext.Provider value={names}>{children}</ActorNamesContext.Provider>
}

/** Match complete IDs only; prose and unknown references retain their original text. */
export function useActorReferenceNames() {
  return useContext(ActorNamesContext)
}
