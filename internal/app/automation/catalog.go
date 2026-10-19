package automationapp

import (
	"fmt"
	"log/slog"

	"golang.org/x/sync/errgroup"

	"denova/internal/automation"
)

// ProjectCatalog is a read-only projection of one registered Project. Failures
// stay local to that Project; no catalog data becomes a second recovery source.
type ProjectCatalog struct {
	ProjectID string                        `json:"project_id"`
	Tasks     []automation.Task             `json:"tasks"`
	Inbox     []automation.TriggerInboxItem `json:"inbox"`
	Error     string                        `json:"error,omitempty"`
}

// CatalogSnapshot reads each Project Store once instead of repeatedly scanning
// the whole registry for every Project's tasks and inbox. Execution and mutation
// remain scoped to the owning Project Agent.
func (s *Service) CatalogSnapshot() ([]ProjectCatalog, error) {
	catalog, err := s.host.Catalog()
	if err != nil {
		return nil, err
	}
	groups := make([]ProjectCatalog, len(catalog.Projects))
	var reads errgroup.Group
	// Project Stores are independent. Bound concurrent disk work and write each
	// result into its original slot to preserve catalog order and local failures.
	reads.SetLimit(8)
	for index, location := range catalog.Projects {
		reads.Go(func() error {
			group := &groups[index]
			*group = ProjectCatalog{ProjectID: location.ProjectID, Tasks: []automation.Task{}, Inbox: []automation.TriggerInboxItem{}}
			defer func() {
				if recovered := recover(); recovered != nil {
					group.Tasks, group.Inbox = []automation.Task{}, []automation.TriggerInboxItem{}
					group.Error = fmt.Sprintf("read Project automation catalog panic: %v", recovered)
					slog.Error("[automation] failed to read Project catalog", "project_id", location.ProjectID, "err", group.Error)
				}
			}()
			store := automation.NewProjectStore(catalog.DataDir, location.ProjectID, location.Workspace, location.StateRoot)
			var err error
			group.Tasks, err = s.projectTasks(store.ListForTarget(automation.ExecutionTarget{
				Kind: automation.TargetKindWorkspace, ProjectID: location.ProjectID, Workspace: location.Workspace,
			}))
			if err == nil {
				group.Inbox, err = store.ListInbox()
			}
			if err != nil {
				group.Tasks, group.Inbox = []automation.Task{}, []automation.TriggerInboxItem{}
				group.Error = fmt.Sprintf("read Project automation catalog: %v", err)
				slog.Error("[automation] failed to read Project catalog", "project_id", location.ProjectID, "err", err)
			}
			return nil
		})
	}
	_ = reads.Wait() // Errors are reported on their owning Project, never dropped.
	return groups, nil
}
