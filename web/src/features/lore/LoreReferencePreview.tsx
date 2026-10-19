import { ArrowLeft, ArrowUpRight } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Sheet, SheetContent, SheetHeader, SheetTitle, SheetDescription } from '@/components/ui/sheet'
import { loreImageURL, type LoreItem } from '@/lib/api'
import { LoreMarkdownEditor } from './LoreMarkdownEditor'
import { LoreReferences } from './LoreReferences'
import { loreNameKey } from './lore-reference'
import { useLoreCategories } from './use-lore-categories'
import { loreTypeLabel } from './options'

/** One preview with local back navigation; source editor and its scroll/undo state stay mounted. */
export function LoreReferencePreview({ projectId, items, history, onHistoryChange, onSelectItem }: {
  projectId: string; items: LoreItem[]; history: string[]; onHistoryChange: (history: string[]) => void
  /** The owning editor saves its draft before selecting the target. */
  onSelectItem: (id: string) => void
}) {
  const { t } = useTranslation()
  const { categories } = useLoreCategories(projectId)
  const name = history.at(-1) || ''
  const item = items.find(item => loreNameKey(item.name) === loreNameKey(name))
  const open = (target: string) => onHistoryChange([...history, target])
  const image = item ? loreImageURL(projectId, item) : ''
  return (
    <Sheet open={history.length > 0} onOpenChange={open => { if (!open) onHistoryChange([]) }}>
      <SheetContent className="gap-0 data-[side=right]:w-full data-[side=right]:sm:max-w-2xl" data-testid="lore-reference-preview">
        <SheetHeader className="shrink-0 border-b pr-12">
          {history.length > 1 && <Button variant="ghost" size="sm" className="self-start" onClick={() => onHistoryChange(history.slice(0, -1))}><ArrowLeft data-icon="inline-start" />{t('lore.references.back')}</Button>}
          <SheetTitle className="break-words">{item?.name || name}</SheetTitle>
          <SheetDescription>{t('lore.references.previewHint')}</SheetDescription>
          {item && (
            <Button variant="outline" size="sm" className="self-start" onClick={() => {
              console.info('[lore-reference] navigating to item', { projectId, itemId: item.id })
              onHistoryChange([])
              onSelectItem(item.id)
            }}>
              <ArrowUpRight data-icon="inline-start" />
              {t('lore.references.openItem')}
            </Button>
          )}
        </SheetHeader>
        {item ? (
          <div className="flex min-h-0 flex-1 flex-col overflow-y-auto" key={name}>
            <div className="flex flex-col gap-3 p-4">
              <div className="flex flex-wrap gap-1">
                <Badge variant="secondary">{categories.find(category => category.id === item.type)?.name || loreTypeLabel(item.type, t)}</Badge>
                {!item.enabled && <Badge variant="outline">{t('lore.references.disabled')}</Badge>}
                {item.tags.map(tag => <Badge key={tag} variant="outline">{tag}</Badge>)}
              </div>
              {item.brief_description && <p className="whitespace-pre-wrap break-words text-sm text-muted-foreground">{item.brief_description}</p>}
              {image && <img src={image} alt={item.image?.alt_text || item.name} referrerPolicy="no-referrer" className="max-h-64 max-w-full self-start rounded-md object-contain" />}
            </div>
            {item.content ? (
              <LoreMarkdownEditor
                projectId={projectId} mode="rich" value={item.content} onChange={() => {}} readOnly
                items={items} onOpenReference={open} aria-label={t('lore.references.content', { name: item.name })}
                className="px-4 pb-4 [&_.tiptap]:outline-none" />
            ) : <p className="p-4 text-sm text-muted-foreground">{t('lore.references.emptyContent')}</p>}
            <LoreReferences items={items} id={item.id} content={item.content} onOpen={open} />
          </div>
        ) : <p role="status" className="p-4 text-sm text-destructive">{t('lore.references.missing', { name })}</p>}
      </SheetContent>
    </Sheet>
  )
}
