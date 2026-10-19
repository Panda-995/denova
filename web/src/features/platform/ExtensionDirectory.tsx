import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { ChevronDown, Gamepad2, Puzzle, Search } from 'lucide-react'
import { InputGroup, InputGroupAddon, InputGroupInput } from '@/components/ui/input-group'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from '@/components/ui/card'
import { Empty, EmptyHeader, EmptyMedia, EmptyTitle, EmptyDescription } from '@/components/ui/empty'
import { localized } from './api'
import type { ExtensionEntry } from './extension-directory'
import { InstalledExtensionToggle } from './InstalledExtensionToggle'

export function ExtensionDirectory({ entries, onSelect, onChanged }: { entries: ExtensionEntry[]; onSelect: (key: string) => void; onChanged: () => Promise<void> }) {
  const { t, i18n } = useTranslation()
  const [search, setSearch] = useState('')
  const words = search.trim().toLocaleLowerCase().split(/\s+/).filter(Boolean)
  const filtered = entries.filter(entry => {
    const text = [localized(entry.manifest?.name, i18n.language), localized(entry.manifest?.description, i18n.language), entry.installed.id].join(' ').toLocaleLowerCase()
    return words.every(word => text.includes(word))
  })
  return <>
    <InputGroup><InputGroupInput aria-label={t('platform.searchExtensions')} placeholder={t('platform.searchExtensions')} value={search} onChange={event => setSearch(event.target.value)} /><InputGroupAddon><Search /></InputGroupAddon></InputGroup>
    {words.length > 0 && !filtered.length ? <Empty><EmptyHeader><EmptyMedia variant="icon"><Search /></EmptyMedia><EmptyTitle>{t('platform.extensions.noResults')}</EmptyTitle><EmptyDescription>{t('platform.extensions.noResultsHelp')}</EmptyDescription></EmptyHeader><Button variant="outline" onClick={() => setSearch('')}>{t('platform.extensions.clearSearch')}</Button></Empty> :
      (['plugin', 'game'] as const).map(kind => {
        const items = filtered.filter(entry => entry.kind === kind)
        const Icon = kind === 'game' ? Gamepad2 : Puzzle
        return <Collapsible key={kind} defaultOpen className="flex flex-col gap-3">
          <CollapsibleTrigger className="flex w-full items-center gap-2 py-2 text-sm font-medium [&[data-state=closed]>svg:last-child]:-rotate-90">
            <Icon className="size-4" /><span>{t('platform.type.' + kind)}</span><Badge variant="secondary">{items.length}</Badge><ChevronDown className="ml-auto size-4" />
          </CollapsibleTrigger>
          <CollapsibleContent>
            <ul className="grid grid-cols-[repeat(auto-fill,minmax(min(100%,18rem),1fr))] gap-3">{items.map(entry => {
              const name = localized(entry.manifest?.name, i18n.language) || entry.installed.id
              return <li key={entry.key} className="min-w-0">
                <Card size="sm" data-testid="installed-extension-card" className="relative h-full min-w-0 transition-shadow hover:ring-foreground/25 focus-within:ring-2 focus-within:ring-ring">
                  <CardHeader className="min-w-0 gap-2">
                    <CardTitle className="flex min-w-0 items-center gap-2"><Icon className="size-4 shrink-0 text-muted-foreground" />
                      <button className="min-w-0 flex-1 truncate text-left after:absolute after:inset-0 focus:outline-none" title={name} onClick={() => onSelect(entry.key)}>{name}</button>
                    </CardTitle>
                    <CardDescription className="line-clamp-2 h-10 break-words">{localized(entry.manifest?.description, i18n.language) || t(`platform.extensions.${kind}Help`)}</CardDescription>
                  </CardHeader>
                  <CardContent className="flex min-w-0 items-center gap-2 text-xs text-muted-foreground">
                    <Badge variant="secondary">{t('platform.type.' + kind)}</Badge>
                    <span className="truncate" title={entry.installed.id}>{entry.installed.id}</span>
                  </CardContent>
                  <CardFooter className="mt-auto justify-between gap-2 py-2 text-xs text-muted-foreground">
                    <span className="min-w-0 truncate">{entry.manifest && `v${entry.manifest.version}`}</span>
                    <div className="relative z-10"><InstalledExtensionToggle item={entry.installed} onChanged={onChanged} /></div>
                  </CardFooter>
                </Card>
              </li>
            })}</ul>
            {!items.length && <p className="p-3 text-sm text-muted-foreground">{t('platform.directoryEmpty')}</p>}
          </CollapsibleContent>
        </Collapsible>
      })}
  </>
}
