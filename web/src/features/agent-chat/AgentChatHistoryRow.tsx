import { Download, Edit3, LoaderCircle, MessageCircle, MoreHorizontal, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { formatDateTime } from '@/i18n'
import { HighlightedText } from '@/components/common/HighlightedText'
import type { AgentChatHistoryItem } from './api'

interface AgentChatHistoryRowProps {
  item: AgentChatHistoryItem
  query: string
  exporting: boolean
  onOpen: () => void
  onRename: () => void
  onDelete: () => void
  onExport: () => void
}

/** One dense history result; project ownership is supplied by the master pane. */
export function AgentChatHistoryRow({ item, query, exporting, onOpen, onRename, onDelete, onExport }: AgentChatHistoryRowProps) {
  const { t } = useTranslation()
  const title = item.session.title || t('chat.untitledSession')
  return (
    <div className="group flex min-w-0 items-center gap-0.5 rounded-[var(--nova-radius)] pr-0.5 hover:bg-[var(--nova-hover)]">
      <button
        type="button"
        onClick={onOpen}
        aria-label={t('agentChat.history.openSession', { title })}
        className="flex min-h-8 min-w-0 flex-1 flex-col gap-1 rounded-[var(--nova-radius)] px-2 py-1.5 text-left outline-none focus-visible:ring-1 focus-visible:ring-[var(--nova-accent)]"
      >
        <span className="flex w-full min-w-0 items-center gap-2">
          <MessageCircle className={`size-3.5 shrink-0 ${item.session.running ? 'text-[var(--nova-success)]' : 'text-[var(--nova-text-faint)]'}`} />
          <span className="min-w-0 flex-1 truncate text-xs text-[var(--nova-text)]">{title}</span>
          {item.session.running ? (
            <span className="inline-flex shrink-0 items-center gap-1 text-[9px] text-[var(--nova-success)]">
              <span aria-hidden="true" className="size-1.5 animate-pulse rounded-full bg-current" />
              {t('agentChat.sidebar.status.running')}
            </span>
          ) : null}
          <time
            dateTime={item.session.updated_at || item.session.created_at}
            className="shrink-0 text-[10px] tabular-nums text-[var(--nova-text-faint)]"
          >
            {formatDateTime(item.session.updated_at || item.session.created_at)}
          </time>
          <span className="hidden shrink-0 text-[10px] tabular-nums text-[var(--nova-text-faint)] sm:inline">
            {t('agentChat.history.messageCount', { count: item.session.message_count })}
          </span>
        </span>
        {item.excerpt ? (
          <span className="w-full truncate pl-5.5 text-xs text-[var(--nova-text-muted)]">
            <HighlightedText text={item.excerpt} query={query} />
          </span>
        ) : null}
      </button>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button
            type="button"
            variant="ghost"
            size="icon-xs"
            className="shrink-0 transition-opacity sm:opacity-0 group-hover:opacity-100 data-[state=open]:opacity-100 focus-visible:opacity-100"
            aria-label={t('agentChat.sidebar.sessionActions', { title })}
          >
            <MoreHorizontal />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" className="min-w-36">
          <DropdownMenuGroup>
            <DropdownMenuItem onSelect={onRename}>
              <Edit3 />
              {t('common.rename')}
            </DropdownMenuItem>
            <DropdownMenuItem disabled={exporting} onSelect={onExport}>
              {exporting ? <LoaderCircle className="animate-spin" /> : <Download />}
              {t(exporting ? 'agentChat.history.exporting' : 'agentChat.history.exportMarkdown')}
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem variant="destructive" disabled={item.session.running} onSelect={onDelete}>
              <Trash2 />
              {t('common.delete')}
            </DropdownMenuItem>
          </DropdownMenuGroup>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  )
}
