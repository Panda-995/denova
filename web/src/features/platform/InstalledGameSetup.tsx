import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Field, FieldDescription, FieldLabel } from '@/components/ui/field'
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { useGameStories, type GameChoice } from './game-story-context'
import { platformError } from './api'
import { RuntimeSetup, emptySetup, type Setup } from './RuntimeSetup'

export function InstalledGameSetup({ game, projectId, onCreate }: { game: GameChoice; projectId: string; onCreate: (title: string, setup: Setup, storyId?: string) => Promise<void> }) {
  const { t } = useTranslation()
  const context = useGameStories()
  const [title, setTitle] = useState('')
  const [storyId, setStoryId] = useState('new')
  const [setup, setSetup] = useState({ ...emptySetup, projectId })
  const [busy, setBusy] = useState(false)
  return <div className="flex min-h-0 flex-1 flex-col overflow-y-auto p-4 sm:p-6">
    <form className="mx-auto flex w-full max-w-xl flex-col gap-6" onSubmit={event => {
      event.preventDefault()
      setBusy(true)
      void onCreate(title, setup, storyId === 'new' ? undefined : storyId).catch(error => toast.error(platformError(error))).finally(() => setBusy(false))
    }}>
      <div className="space-y-2"><h2 className="break-words text-lg font-semibold">{game.name}</h2><p className="text-sm text-muted-foreground">{t('platform.newGameStoryDescription')}</p></div>
      <Field><FieldLabel htmlFor="game-story-title">{t('platform.storyName')}</FieldLabel><Input id="game-story-title" value={title} placeholder={game.name} maxLength={120} onChange={event => setTitle(event.target.value)} /></Field>
      {game.manifest?.game?.storage.kind === 'story' && <Field>
        <FieldLabel htmlFor="game-story-source">{t('platform.storySource')}</FieldLabel>
        <Select value={storyId} onValueChange={setStoryId} disabled={busy}>
          <SelectTrigger id="game-story-source" className="w-full"><SelectValue /></SelectTrigger>
          <SelectContent><SelectGroup>
            <SelectItem value="new">{t('platform.newStorySource')}</SelectItem>
            {context?.picker.stories.filter(story => !story.id.startsWith('game:')).map(story => <SelectItem key={story.id} value={story.id}>{story.title}</SelectItem>)}
          </SelectGroup></SelectContent>
        </Select>
        <FieldDescription>{t('platform.storySourceHelp')}</FieldDescription>
      </Field>}
      <RuntimeSetup configurationEndpoint={`/packages/game/${game.id}/setup?releaseId=${game.releaseId}`} manifest={game.manifest} value={setup} onChange={setSetup} projectLocked />
      <Button type="submit" disabled={busy || !projectId}>{t('platform.createStory')}</Button>
    </form>
  </div>
}
