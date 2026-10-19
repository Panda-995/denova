package execution

import (
	"context"

	"github.com/alfredxw/denova/agent"
	agentsession "github.com/alfredxw/denova/agent/session"
)

// taskSessions discovers only exact descendants of this product Session.
// Requests and answers stay in the owning journal; discovery never resumes a
// Run or copies child interactions into a second durable queue.
func (backend *publicBackend) taskSessions(ctx context.Context, root *agent.Session) ([]*agent.Session, error) {
	sessions := []*agent.Session{root}
	seen := make(map[string]bool)
	for index := 0; index < len(sessions); index++ {
		children, err := sessions[index].PendingChildTasks(ctx)
		if err != nil {
			return nil, err
		}
		keys := make([]agentsession.Key, 0, len(children))
		for _, child := range children {
			keys = append(keys, child.Key)
		}
		for _, key := range keys {
			canonical, err := agentsession.CanonicalKey(key)
			if err != nil {
				return nil, err
			}
			if seen[canonical] {
				continue
			}
			seen[canonical] = true
			child, err := backend.agent.Session(ctx, key)
			if err != nil {
				return nil, err
			}
			sessions = append(sessions, child)
		}
	}
	return sessions, nil
}
