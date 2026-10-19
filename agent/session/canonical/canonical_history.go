package canonical

import (
	"context"
	agenthistory "github.com/alfredxw/denova/agent/context/history"

	agentschema "github.com/alfredxw/denova/agent/schema"
)

// CanonicalHistoryHead identifies the immutable product lane and its current
// model-visible revision. Revision must use the same identity as CommitReceipt;
// display-only appends must not advance it. Neither field may contain host paths.
// Identity must change on Clear or any edit/reordering of an existing prefix,
// including edits to messages currently covered by a compaction checkpoint.
type CanonicalHistoryHead struct {
	Identity string `json:"identity"`
	Revision string `json:"revision"`
}

// CanonicalHistorySource owns the full history needed for reconstruction and
// explicit compaction removal. Aligned checkpoints need only the cheap head.
// Read and Head must observe one canonical lane, including external edits/Clear.
type CanonicalHistorySource interface {
	CanonicalHistoryHead(context.Context) (CanonicalHistoryHead, error)
	CanonicalMessages(context.Context) ([]*agentschema.Message, error)
}

// CanonicalHistoryVisitor streams the same physical message order as
// CanonicalMessages. A visitor must not call back into the source while it is
// walking. Each message is detached and may be retained by the visitor.
// Sources implement this to restore an archived window without keeping
// discarded bodies in memory; the journal remains the sole recovery authority.
type CanonicalHistoryVisitor interface {
	VisitCanonicalMessages(context.Context, func(*agentschema.Message) error) error
}

// CanonicalHistoryWindow returns the retained window in semantic context-state
// order. Coordinates count the complete lane; omitted bodies need not be read.
// Implementations must verify the full lane count before selecting the window.
type CanonicalHistoryWindow interface {
	ReadCanonicalWindow(context.Context, int, *agenthistory.HistoryArchive) ([]*agentschema.Message, error)
}
