package interactiveapp

import (
	"context"
	"fmt"

	agentrun "denova/internal/agents/run"
	"denova/internal/agents/toolresult"
	"denova/internal/interactive"

	agenthistory "github.com/alfredxw/denova/agent/context/history"
	agentschema "github.com/alfredxw/denova/agent/schema"
	agentcanonical "github.com/alfredxw/denova/agent/session/canonical"
)

func (c *Conversation) ReadCanonicalWindow(ctx context.Context, count int, archive *agenthistory.HistoryArchive) ([]*agentschema.Message, error) {
	storyContext, err := c.storyContextForCycle()
	if err != nil {
		return nil, err
	}
	snapshot := storyContext.Snapshot
	snapshot.TurnCount = SnapshotTurnCount(snapshot)
	return c.store.ReadCanonicalWindow(ctx, c.storyID, snapshot, count, archive)
}

func (c *Conversation) CanonicalHistoryHead(ctx context.Context) (agentcanonical.CanonicalHistoryHead, error) {
	head, err := c.store.CanonicalHistoryHead(ctx, c.storyID, c.branchID)
	if target := c.regenerateTargetSnapshot(); target != "" {
		// Regeneration reads the target's parent instead of the current branch.
		head.Identity += "/regenerate/" + target
	}
	return head, err
}

// CanonicalMessages projects the exact Story branch model history. Story JSONL
// is the sole durable conversation lane, including Agent's bounded checkpoints.
func (c *Conversation) CanonicalMessages(ctx context.Context) ([]*agentschema.Message, error) {
	if c == nil || c.store == nil {
		return nil, fmt.Errorf("interactive canonical transcript is unavailable")
	}
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	storyContext, err := c.storyContextForCycle()
	if err != nil {
		return nil, err
	}
	return c.canonicalMessagesForSnapshot(storyContext.Snapshot)
}

func (c *Conversation) canonicalMessagesForSnapshot(snapshot interactive.Snapshot) ([]*agentschema.Message, error) {
	turnCount := SnapshotTurnCount(snapshot)
	history, err := c.store.ReadModelHistory(c.storyID, interactive.StoryModelHistoryQuery{
		BranchID: snapshot.BranchID, StartTurn: 0, EndTurn: turnCount,
	})
	if err != nil {
		return nil, err
	}
	// Import the complete canonical branch so incremental Compaction targets
	// stable raw message indices instead of a second Story-store projection.
	projection, err := buildModelContextProjection(
		history, nil, snapshot,
		canonicalToolContextPolicy(c.ToolResultContextPolicy()), agentrun.CycleIdentity{},
		func(input interactive.PlayerInputAcceptedEvent) *agentschema.Message {
			return agentschema.UserMessageWithAttachments(input.Text, input.Attachments)
		},
	)
	if err != nil {
		return nil, err
	}
	return projection.Messages, nil
}

func canonicalToolContextPolicy(policy toolresult.ContextPolicy) toolresult.ContextPolicy {
	// Product visibility preferences never erase canonical raw history. The
	// model-call middleware applies Enabled on a per-request projection, while
	// Compaction and remove/rebuild continue to address the complete
	// validated tool batch stored by public Agent.
	policy.Enabled = true
	return policy
}
