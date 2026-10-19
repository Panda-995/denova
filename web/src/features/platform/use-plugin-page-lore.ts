import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import type { AgentPanelProps } from '@/components/Chat/AgentPanel'
import type { EditorFlushHandler } from '@/components/Editor/useEditorDraftPersistence'
import type { DocumentReviewNavigationIntent } from '@/features/document-review/controller'
import { useDocumentReview } from '@/features/document-review/use-document-review'

export type PluginPageLoreAgentProps = Pick<AgentPanelProps,
  | 'loreReferences' | 'onLoreReferenceRemove' | 'reviewFeedback'
  | 'onReviewFeedbackOpen' | 'onReviewFeedbackRemove'
  | 'onReviewFeedbackSubmitted' | 'onReviewFeedbackSubmissionFailed'
>

/** Native lore has the same Project and conversation as its plugin page. The
 * reference queue hands IDs to the existing conversation-owned draft exactly once. */
export function usePluginPageLore({ projectId, agentVisible, onShowAgent, onShowContent, onFlushHandlerChange }: {
  projectId: string
  agentVisible: boolean
  onShowAgent: () => void
  onShowContent: () => void
  onFlushHandlerChange?: (handler: EditorFlushHandler | null) => void
}) {
  const [open, setOpen] = useState(false)
  const [activeItemId, setActiveItemId] = useState<string | null>(null)
  const [references, setReferences] = useState<string[]>([])
  const [navigation, setNavigation] = useState<DocumentReviewNavigationIntent | null>(null)
  const editorFlush = useRef<EditorFlushHandler | null>(null)
  const onEditorFlushHandlerChange = useCallback((handler: EditorFlushHandler | null) => { editorFlush.current = handler }, [])
  const flush = useCallback(async () => editorFlush.current ? editorFlush.current() : true, [])
  useEffect(() => {
    onFlushHandlerChange?.(flush)
    return () => onFlushHandlerChange?.(null)
  }, [flush, onFlushHandlerChange])
  const openTab = useCallback(() => setOpen(true), [])
  const closeTab = useCallback(async () => {
    if (!(await flush())) return false
    setOpen(false)
    return true
  }, [flush])
  const referenceItem = useCallback(async (id: string) => {
    // A reference must resolve to the just-edited canonical content when sent.
    if (!(await flush())) return
    setReferences(current => current.includes(id) ? current : [...current, id])
    onShowAgent()
  }, [flush, onShowAgent])
  const review = useDocumentReview({ projectId, agentVisible, onShowAgent })
  const documentReview = useMemo(() => ({
    comments: review.visibleComments,
    onCreate: review.addComment,
    onUpdate: review.editComment,
    onDelete: review.removeComment,
  }), [review.visibleComments, review.addComment, review.editComment, review.removeComment])
  const removeReference = useCallback((id: string) => setReferences(current => current.filter(value => value !== id)), [])
  // Workspace-file comments belong to the Writing surface, which can navigate
  // those files. This surface exposes the same queue for lore comments only.
  const feedbackComments = review.feedback?.comments.filter(comment => comment.target?.kind === 'lore_item') ?? []
  const agentProps: PluginPageLoreAgentProps = {
    loreReferences: references,
    onLoreReferenceRemove: removeReference,
    reviewFeedback: review.feedback && feedbackComments.length ? [{ ...review.feedback, comments: feedbackComments }] : [],
    onReviewFeedbackOpen: (_selection, comment) => {
      setNavigation(current => ({ commentID: comment.id, nonce: (current?.nonce ?? 0) + 1 }))
      setOpen(true)
      onShowContent()
    },
    onReviewFeedbackRemove: (_selection, id) => review.removeFeedback(id),
    onReviewFeedbackSubmitted: feedback => feedback.forEach(review.submitFeedback),
    onReviewFeedbackSubmissionFailed: feedback => feedback.forEach(review.restoreFeedback),
  }
  return { open, openTab, closeTab, activeItemId, setActiveItemId, referenceItem,
    documentReview, navigation, onEditorFlushHandlerChange, agentProps }
}
