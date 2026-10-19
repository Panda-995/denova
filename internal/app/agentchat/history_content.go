package agentchat

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"denova/internal/agents/session"
	"denova/internal/i18n"
)

const historyContentPageSize = 100

// ExportMarkdown exports the saved creator-facing conversation without tools,
// reasoning or model-only context. Reading backwards from the first page fixes
// the export's tail even if a running Agent appends more messages. Page strings
// are joined in chronological order; attachment bytes are never embedded.
func (service *Service) ExportMarkdown(ctx context.Context, binding Binding, locale string) ([]byte, error) {
	binding, err := service.ResolveBinding(binding)
	if err != nil {
		return nil, err
	}
	_, layout, err := service.registry.Resolve(binding.ProjectID, true)
	if err != nil {
		return nil, err
	}
	store, err := service.projectSessionStore(binding.ProjectID, layout.SessionsDir(), false)
	if err != nil {
		return nil, err
	}
	sess, err := store.Get(binding.SessionID)
	if err != nil {
		return nil, err
	}
	startedAt := time.Now()
	localizer := i18n.New(locale)
	title := strings.Join(strings.Fields(sess.Title()), " ")
	if title == "" {
		title = localizer.T("api.agentChat.untitled")
	}
	pages := make([]string, 0)
	for before := -1; ; {
		page, err := sess.ReadHistoryPage(ctx, before, historyContentPageSize)
		if err != nil {
			return nil, err
		}
		var block strings.Builder
		for _, entry := range page.Entries {
			if !session.IsTranscriptMessage(entry) {
				continue
			}
			role := localizer.T("api.agentChat.exportAssistant")
			if entry.Role == "user" {
				role = localizer.T("api.agentChat.exportUser")
			}
			fmt.Fprintf(&block, "## %s\n\n%s\n\n", role, entry.Content)
			for _, attachment := range entry.Attachments {
				name := strings.Join(strings.Fields(attachment.Name), " ")
				fmt.Fprintf(&block, "%s\n\n", localizer.T("api.agentChat.exportAttachment", "name", name))
			}
		}
		pages = append(pages, block.String())
		if !page.HasMore {
			break
		}
		before = page.NextBefore
	}
	var output strings.Builder
	fmt.Fprintf(&output, "# %s\n\n", title)
	for index := len(pages) - 1; index >= 0; index-- {
		output.WriteString(pages[index])
	}
	slog.InfoContext(ctx, "Exported conversation Markdown", "project_id", binding.ProjectID,
		"session_id", binding.SessionID, "bytes", output.Len(), "duration", time.Since(startedAt))
	return []byte(output.String()), nil
}
