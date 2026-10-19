import { useMemo } from 'react'
import { ChevronDown } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import type { LoreItem } from '@/lib/api'
import { loreNameKey, loreReferenceNames } from './lore-reference'

export function useLoreBacklinks(items: LoreItem[], name: string, id: string) {
  return useMemo(() => items.filter(item => item.id !== id && loreReferenceNames(item.content).some(
    reference => loreNameKey(reference) === loreNameKey(name),
  )), [items, name, id])
}

/** Warn before editing the name; renaming intentionally does not rewrite user prose. */
export function LoreRenameNotice({ items, id }: { items: LoreItem[]; id: string }) {
  const { t } = useTranslation()
  const item = items.find(item => item.id === id)
  const backlinks = useLoreBacklinks(items, item?.name || '', id)
  return backlinks.length > 0 ? (
    <p className="text-xs text-muted-foreground" role="note">
      {t('lore.references.renameNotice', { count: backlinks.length })}
    </p>
  ) : null
}

export function LoreReferences({ items, id, content, onOpen }: {
  items: LoreItem[]; id: string; content: string; onOpen: (name: string) => void
}) {
  const { t } = useTranslation()
  const item = items.find(item => item.id === id)
  const incoming = useLoreBacklinks(items, item?.name || '', id)
  const outgoing = useMemo(() => loreReferenceNames(content), [content])
  return (
    <Collapsible className="shrink-0 border-t" data-testid="lore-references">
      <CollapsibleTrigger asChild>
        <Button variant="ghost" className="w-full justify-between" size="sm">
          {t('lore.references.summary', { outgoing: outgoing.length, incoming: incoming.length })}
          <ChevronDown data-icon="inline-end" />
        </Button>
      </CollapsibleTrigger>
      <CollapsibleContent className="max-h-48 overflow-y-auto px-3 pb-3">
        {([
          { key: 'outgoing', names: outgoing },
          { key: 'incoming', names: incoming.map(item => item.name) },
        ] as const).map(group => (
          <div key={group.key} className="flex flex-col gap-1 py-1">
            <p className="text-xs text-muted-foreground">{t(`lore.references.${group.key}`)}</p>
            {group.names.length === 0 && <p className="text-xs text-muted-foreground">{t('lore.references.empty')}</p>}
            <div className="flex flex-wrap gap-1">
              {group.names.map(name => (
                <Button key={name} size="sm" variant="outline" className="h-auto max-w-full whitespace-normal break-words" onClick={() => onOpen(name)}>
                  {name}
                  {!items.some(item => loreNameKey(item.name) === loreNameKey(name)) && <span className="text-destructive">{t('lore.references.missingLabel')}</span>}
                </Button>
              ))}
            </div>
          </div>
        ))}
      </CollapsibleContent>
    </Collapsible>
  )
}
