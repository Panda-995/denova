import { useState, type ReactNode } from 'react'
import loadable from '@loadable/component'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { LoadingState } from '@/components/common/LoadingState'
import { Field, FieldLabel } from '@/components/ui/field'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { useGameStories } from './game-story-context'
import { BUILTIN_GAME_ID, platformError } from './api'

// Package configuration includes a schema form engine; native stories do not need it.
const InstalledGameSetup = loadable(() => import('./InstalledGameSetup'), { resolveComponent: module => module.InstalledGameSetup })

/** The selector is part of new-story setup, never a separate navigation step. */
export function GameStorySetup({ enabled, children }: { enabled: boolean; children: ReactNode }) {
  const context = useGameStories()
  const { t } = useTranslation()
  const [saving, setSaving] = useState(false)
  if (!enabled || !context) return children
  const game = context.choices.find(game => game.id === context.selectedGameId)!
  return <div className="flex min-h-0 flex-1 flex-col">
    {context.choices.length > 1 && <div className="flex flex-wrap items-end gap-2 border-b px-4 py-3 sm:px-6">
      <Field className="min-w-0 flex-1 sm:max-w-sm">
        <FieldLabel htmlFor="story-game-type">{t('platform.gameType')}</FieldLabel>
        <Select value={context.selectedGameId} onValueChange={context.chooseGame}>
          <SelectTrigger id="story-game-type" className="w-full"><SelectValue /></SelectTrigger>
          <SelectContent>{context.choices.map(choice => <SelectItem key={choice.id} value={choice.id}>{choice.name}</SelectItem>)}</SelectContent>
        </Select>
      </Field>
      <Button variant="ghost" size="sm" disabled={saving || context.defaultGameId === context.selectedGameId} onClick={() => {
        setSaving(true)
        void context.setDefault().catch(error => toast.error(platformError(error))).finally(() => setSaving(false))
      }}>{t(context.defaultGameId === context.selectedGameId ? 'platform.defaultGame' : 'platform.setDefaultGame')}</Button>
    </div>}
    {game.id === BUILTIN_GAME_ID ? children : <InstalledGameSetup key={game.id} game={game} projectId={context.projectId} onCreate={context.createInstalled} fallback={<LoadingState label={t('common.loading')} />} />}
  </div>
}
