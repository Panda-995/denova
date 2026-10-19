import { useMemo, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { History, X } from 'lucide-react'
import { Virtuoso } from 'react-virtuoso'
import { useIsMobile } from '@/hooks/useIsMobile'
import { Button } from '@/components/ui/button'
import { Sheet, SheetContent, SheetTitle } from '@/components/ui/sheet'

export interface TurnNavigationItem {
  anchorId: string
  user: string
  narrative: string
  contextOnly?: boolean
  pending?: boolean
}

export interface TurnNavigatorProps {
  items: TurnNavigationItem[]
  activeAnchorId?: string
  onSelect: (anchorId: string) => void
  earlierCount?: number
  loadingEarlier?: boolean
  onSelectEarlier?: (index: number) => void
  /** Keep the history sheet mounted when a title menu closes after selection. */
  renderTrigger?: (openHistory: () => void) => ReactNode
}

const MAX_TURN_NAVIGATION_MARKS = 28

interface AggregatedTurnNavigationItem {
  item?: TurnNavigationItem
  sourceIndex: number
}

export function TurnNavigator({ items, activeAnchorId = '', onSelect, earlierCount = 0, loadingEarlier = false, onSelectEarlier, renderTrigger }: TurnNavigatorProps) {
  const { t } = useTranslation()
  const [previewAnchorId, setPreviewAnchorId] = useState('')
  const isMobile = useIsMobile()
  const [historyOpen, setHistoryOpen] = useState(false)
  const navigationItems = useMemo(
    () => aggregateTurnNavigationItems(items, activeAnchorId, MAX_TURN_NAVIGATION_MARKS, earlierCount),
    [activeAnchorId, earlierCount, items],
  )
  if (items.length === 0 && !renderTrigger) return null

  if (isMobile) {
    return (
      <>
        {renderTrigger ? renderTrigger(() => setHistoryOpen(true)) : <Button type="button" variant="ghost" size="icon" aria-label={t('storyStage.turnNavigator.label')} title={t('storyStage.mobile.history')} onClick={() => setHistoryOpen(true)}>
          <History />
        </Button>}
        <Sheet open={historyOpen} onOpenChange={setHistoryOpen}>
          <SheetContent side="bottom" showCloseButton={false} aria-describedby={undefined} className="nova-mobile-navigation-sheet gap-0 rounded-t-2xl p-0">
            <header className="flex min-h-14 shrink-0 items-center justify-between border-b px-4">
              <SheetTitle>{t('storyStage.turnNavigator.label')}</SheetTitle>
              <Button variant="ghost" size="icon" aria-label={t('common.close')} onClick={() => setHistoryOpen(false)}><X /></Button>
            </header>
            <Virtuoso className="p-3" style={{ height: 'min(65dvh, 640px)' }} totalCount={earlierCount + items.length}
              computeItemKey={index => items[index - earlierCount]?.anchorId || `earlier-${index}`}
              itemContent={index => {
                const item = items[index - earlierCount]
                return <button type="button" aria-label={t('storyStage.turnNavigator.goto', { index: index + 1 })} aria-current={item?.anchorId === activeAnchorId ? 'true' : undefined}
                  disabled={!item && loadingEarlier}
                  className="flex w-full min-w-0 items-start gap-3 rounded-xl p-3 text-left text-sm active:bg-accent aria-current:bg-accent"
                  onClick={() => { setHistoryOpen(false); if (item) onSelect(item.anchorId); else onSelectEarlier?.(index) }}>
                  <span className="shrink-0 tabular-nums text-muted-foreground">{index + 1}</span>
                  <span className="min-w-0 flex-1">
                    <span className="block truncate font-medium">{!item ? t('storyStage.turnNavigator.loadTurn') : item.contextOnly ? t('storyStage.turnNavigator.autonomousContinuation') : item.user.trim() || t('storyStage.turnNavigator.emptyUser')}</span>
                    {item ? <span className="mt-1 line-clamp-2 break-words text-muted-foreground">{item.narrative.trim() || t(item.pending ? 'storyStage.turnNavigator.generating' : 'storyStage.turnNavigator.emptyAgent')}</span> : null}
                  </span>
                </button>
              }} />
          </SheetContent>
        </Sheet>
      </>
    )
  }

  return (
    <aside className="nova-turn-navigator" aria-label={t('storyStage.turnNavigator.label')}>
      <div className="nova-turn-navigator-track" role="list">
        {navigationItems.map(({ item, sourceIndex }) => {
          const anchorId = item?.anchorId || `earlier-${sourceIndex}`
          const active = item?.anchorId === activeAnchorId
          const previewVisible = previewAnchorId === anchorId
          const user = !item ? t('storyStage.turnNavigator.loadTurn') : item.contextOnly
            ? t('storyStage.turnNavigator.autonomousContinuation')
            : item.user.trim() || t('storyStage.turnNavigator.emptyUser')
          const narrative = item ? item.narrative.trim() || (item.pending ? t('storyStage.turnNavigator.generating') : t('storyStage.turnNavigator.emptyAgent')) : ''
          return (
            <div key={anchorId} className="nova-turn-nav-slot" role="listitem" aria-posinset={sourceIndex + 1} aria-setsize={earlierCount + items.length}>
              <button
                type="button"
                className="nova-turn-nav-button"
                aria-current={active ? 'true' : undefined}
                aria-label={t('storyStage.turnNavigator.goto', { index: sourceIndex + 1 })}
                data-active={active ? 'true' : undefined}
                data-pending={item?.pending ? 'true' : undefined}
                disabled={!item && loadingEarlier}
                onClick={() => { if (item) onSelect(item.anchorId); else onSelectEarlier?.(sourceIndex) }}
                onMouseEnter={() => setPreviewAnchorId(anchorId)}
                onMouseLeave={() => setPreviewAnchorId((current) => (current === anchorId ? '' : current))}
                onFocus={() => setPreviewAnchorId(anchorId)}
                onBlur={() => setPreviewAnchorId((current) => (current === anchorId ? '' : current))}
              >
                <span className="nova-turn-nav-mark" aria-hidden="true" />
                {previewVisible ? (
                  <span className="nova-turn-nav-preview" aria-hidden="true">
                    <span className="nova-turn-nav-preview-user">{user}</span>
                    <span className="nova-turn-nav-preview-agent">{narrative}</span>
                  </span>
                ) : null}
              </button>
            </div>
          )
        })}
      </div>
    </aside>
  )
}

export function aggregateTurnNavigationItems(
  items: TurnNavigationItem[],
  activeAnchorId = '',
  maxMarks = MAX_TURN_NAVIGATION_MARKS,
  earlierCount = 0,
): AggregatedTurnNavigationItem[] {
  const total = earlierCount + items.length
  if (total <= maxMarks || maxMarks < 3) {
    return Array.from({ length: total }, (_, sourceIndex) => ({ item: items[sourceIndex - earlierCount], sourceIndex }))
  }
  const selected = new Set<number>([0, total - 1])
  const activeIndex = items.findIndex((item) => item.anchorId === activeAnchorId)
  if (activeIndex >= 0) selected.add(earlierCount + activeIndex)
  items.forEach((item, index) => {
    if (item.pending) selected.add(earlierCount + index)
  })
  for (let slot = 0; slot < maxMarks; slot += 1) {
    selected.add(Math.round((slot * (total - 1)) / (maxMarks - 1)))
  }
  return [...selected]
    .sort((left, right) => left - right)
    .map((sourceIndex) => ({ item: items[sourceIndex - earlierCount], sourceIndex }))
}
