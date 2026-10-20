package app

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	agentchat "denova/internal/agents/chat"
	agentreview "denova/internal/agents/review"
	workspacechange "denova/internal/workspace/change"
	"denova/internal/workspace/documentreview"
)

func TestDocumentReviewFeedbackRetainsHistoricalQuotesAfterEdits(t *testing.T) {
	const before = "Alpha target Omega\nUnchanged note\n"
	for _, test := range []struct {
		name  string
		after string
	}{
		{name: "surrounding text changed", after: "New context target Omega\nUnchanged note\n"},
		{name: "quoted text rewritten", after: "Alpha revised image Omega\nUnchanged note\n"},
		{name: "quoted text deleted", after: "Unchanged note\n"},
		{name: "quoted context duplicated", after: "Alpha target Omega\nAlpha target Omega\nUnchanged note\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			workspace := t.TempDir()
			path := "chapter.md"
			file := filepath.Join(workspace, path)
			if err := os.WriteFile(file, []byte(before), 0o644); err != nil {
				t.Fatal(err)
			}
			reviews, err := documentreview.ForWorkspace(workspace)
			if err != nil {
				t.Fatal(err)
			}
			snapshot := documentreview.Snapshot{Content: before, Revision: workspacechange.Revision([]byte(before))}
			anchor := documentreview.Anchor{
				Kind: documentreview.AnchorKindTextRange, Encoding: documentreview.AnchorEncodingUTF8,
				Revision: snapshot.Revision, Start: len("Alpha "), End: len("Alpha target"),
				Quote: "target", Prefix: "Alpha ", Suffix: " Omega\n", DisplayQuote: "target",
			}
			thread, historical, err := reviews.AddComment(ctx, documentreview.AddCommentRequest{
				Target: documentreview.Target{Kind: documentreview.TargetKindWorkspaceFile, ID: path},
				Body:   "Make this image more specific.", Anchor: anchor,
			}, snapshot)
			if err != nil {
				t.Fatal(err)
			}
			_, current, err := reviews.AddComment(ctx, documentreview.AddCommentRequest{
				Target: historical.Target, Body: "Keep this note.",
				Anchor: documentreview.Anchor{
					Kind: anchor.Kind, Encoding: anchor.Encoding, Revision: anchor.Revision,
					Start: len("Alpha target Omega\n"), End: len(before) - 1,
					Quote: "Unchanged note", DisplayQuote: "Unchanged note",
				},
			}, snapshot)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(file, []byte(test.after), 0o644); err != nil {
				t.Fatal(err)
			}
			chat := &ChatAppService{}
			runtime := bookReviewRuntime(workspace, nil)
			request := agentchat.ChatRequest{ReviewFeedback: agentreview.Refs{{
				Source: agentreview.SourceDocument, ReviewThreadID: thread.ID,
				CommentIDs: []string{historical.ID, current.ID},
			}}}
			if err := chat.resolveReviewFeedback(ctx, runtime, &request); err != nil {
				t.Fatalf("edited source blocked feedback submission: %v", err)
			}
			comments := request.ResolvedReviewFeedback[0].Comments
			if len(comments) != 2 || comments[0].Body != historical.Body || comments[0].Anchor.Quote != anchor.Quote || comments[0].Anchor.Revision != anchor.Revision || comments[1].Body != current.Body {
				t.Fatalf("review instructions or original quote lost: %#v", comments)
			}
			block, ok := request.ResolvedReviewFeedback.ModelContextBlock()
			if !ok || !strings.Contains(block, `"anchor_status":"historical"`) || !strings.Contains(block, `"anchor_status":"current"`) {
				t.Fatalf("model cannot distinguish historical and current anchors: %s", block)
			}
			stored, err := reviews.GetReviewComments(ctx, thread.ID, []string{historical.ID})
			if err != nil || len(stored) != 1 || !reflect.DeepEqual(stored[0].Anchor, anchor) {
				t.Fatalf("projection changed the stored source quote: %#v err=%v", stored, err)
			}
			if err := chat.consumeResolvedReviewFeedback(ctx, runtime, request); err != nil {
				t.Fatal(err)
			}
			pending, err := reviews.CurrentThread(ctx)
			if err != nil || len(pending.Comments) != 0 {
				t.Fatalf("submitted historical feedback remained pending: %#v err=%v", pending, err)
			}
		})
	}
}
