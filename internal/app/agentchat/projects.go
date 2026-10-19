package agentchat

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	agentconversation "denova/internal/agents/conversation"
	"denova/internal/agents/conversationconfig"
	"denova/internal/agents/session"
	projectdomain "denova/internal/project"
)

// ProjectSessionsLimit bounds the returned recent-session window. Running
// conversations are always retained outside that window.
const ProjectSessionsLimit = 50

func (service *Service) CreateDirectoryProject(request projectdomain.CreateDirectoryRequest) (projectdomain.Record, error) {
	return service.registry.CreateDirectory(request)
}

// AddProject registers a directory without changing the foreground Writing
// workspace. Existing project identity and custom naming are preserved.
func (service *Service) AddProject(path string) (projectdomain.Record, error) {
	if service == nil || service.registry == nil {
		return projectdomain.Record{}, fmt.Errorf("project registry is unavailable")
	}
	kind, err := projectdomain.DetectType(path)
	if err != nil {
		return projectdomain.Record{}, err
	}
	if existing, found, findErr := service.registry.FindByPath(path, true); findErr != nil {
		return projectdomain.Record{}, findErr
	} else if found {
		kind = existing.Type
	}
	record, err := service.registry.Add(path, kind, "")
	if err != nil {
		return projectdomain.Record{}, err
	}
	if _, err := service.registry.EnsureStore(record); err != nil {
		return projectdomain.Record{}, err
	}
	return record, nil
}

func (service *Service) RenameProject(id, name string) (projectdomain.Record, error) {
	if service == nil || service.registry == nil {
		return projectdomain.Record{}, fmt.Errorf("project registry is unavailable")
	}
	return service.registry.Rename(id, name)
}

func (service *Service) ReorderProjects(ids []string) error {
	if service == nil || service.registry == nil {
		return fmt.Errorf("project registry is unavailable")
	}
	return service.registry.Reorder(ids)
}

func (service *Service) Projects(projectID string) []Project {
	if service == nil || service.registry == nil {
		return nil
	}
	if err := service.registry.DiscoverBooks(); err != nil {
		slog.Error("Discover Projects failed", "error", err)
		return []Project{}
	}
	startedAt := time.Now()
	currentWorkspace := ""
	if service.host != nil {
		currentWorkspace = service.host.CurrentWorkspace()
	}
	currentWorkspaceKey := canonicalWorkspaceKey(currentWorkspace)
	records, err := service.registry.List(false)
	if err != nil {
		slog.ErrorContext(context.Background(), fmt.Sprintf("[app/agentchat] list projects failed err=%v", err))
		return []Project{}
	}
	runningBindings := service.runningBindingKeys()
	projects := make([]Project, 0, len(records))
	for _, record := range records {
		if projectID != "" && record.ID != projectID {
			continue
		}
		project := Project{
			ID: record.ID, Type: record.Type, Path: record.WorkspacePath,
			Name: record.Name, Status: record.Status,
			Current: record.Type == projectdomain.TypeBook && canonicalWorkspaceKey(record.WorkspacePath) == currentWorkspaceKey,
		}
		layout, layoutErr := service.registry.Layout(record)
		if layoutErr != nil {
			project.Error = layoutErr.Error()
			projects = append(projects, project)
			continue
		}
		metas, readErr := service.readProjectSessions(record.ID, layout.SessionsDir())
		if readErr != nil {
			slog.ErrorContext(context.Background(), fmt.Sprintf("[app/agentchat] read project sessions failed project_id=%s err=%v", record.ID, readErr))
			project.Error = readErr.Error()
			projects = append(projects, project)
			continue
		}
		project.Total = len(metas)
		metas = visibleProjectSessions(metas, record.ID, runningBindings)
		project.Sessions = make([]Session, 0, len(metas))
		for _, meta := range metas {
			project.Sessions = append(project.Sessions, sessionFromMeta(meta, record.ID, runningBindings))
		}
		projects = append(projects, project)
	}
	totalSessions := 0
	for _, project := range projects {
		totalSessions += project.Total
	}
	slog.DebugContext(context.Background(), fmt.Sprintf(
		"[app/agentchat] listed project session metadata projects=%d sessions=%d duration=%s",
		len(projects), totalSessions, time.Since(startedAt),
	))
	return projects
}

func visibleProjectSessions(metas []session.SessionMeta, projectID string, runningBindings map[string]struct{}) []session.SessionMeta {
	if len(metas) <= ProjectSessionsLimit {
		return metas
	}
	visible := append([]session.SessionMeta(nil), metas[:ProjectSessionsLimit]...)
	for _, meta := range metas[ProjectSessionsLimit:] {
		if sessionRunning(projectID, meta.ID, runningBindings) {
			visible = append(visible, meta)
		}
	}
	return visible
}

func sessionRunning(projectID, sessionID string, runningBindings map[string]struct{}) bool {
	_, running := runningBindings[bindingKey(Binding{ProjectID: projectID, SessionID: sessionID})]
	return running
}

func sessionFromMeta(meta session.SessionMeta, projectID string, runningBindings map[string]struct{}) Session {
	customAgentID := ""
	if meta.RuntimeConfig != nil {
		customAgentID = meta.RuntimeConfig.CustomAgentID
	}
	return Session{
		ID: meta.ID, Title: meta.Title, CreatedAt: meta.CreatedAt, UpdatedAt: meta.UpdatedAt,
		MessageCount: meta.MessageCount, Running: sessionRunning(projectID, meta.ID, runningBindings), CustomAgentID: customAgentID,
	}
}

func (service *Service) History(ctx context.Context, query HistoryQuery) (HistoryPage, error) {
	page := HistoryPage{Items: []HistoryItem{}, Offset: max(query.Offset, 0)}
	if service == nil || service.registry == nil || query.Limit <= 0 {
		return page, nil
	}
	startedAt := time.Now()
	normalizedQuery := strings.ToLower(strings.TrimSpace(query.Search))
	projectID := strings.TrimSpace(query.ProjectID)
	runningBindings := service.runningBindingKeys()
	items := make([]HistoryItem, 0)
	var records []projectdomain.Record
	if projectID != "" {
		if record, err := service.registry.Get(projectID); err == nil && record.Status != projectdomain.StatusArchived {
			records = []projectdomain.Record{record}
		}
	} else {
		records, _ = service.registry.List(false)
	}
	for _, record := range records {
		if projectID != "" && record.ID != projectID {
			continue
		}
		layout, layoutErr := service.registry.Layout(record)
		if layoutErr != nil {
			continue
		}
		metas, err := service.readProjectSessions(record.ID, layout.SessionsDir())
		if err != nil {
			slog.ErrorContext(context.Background(), fmt.Sprintf("[app/agentchat] read project history failed project_id=%s err=%v", record.ID, err))
			continue
		}
		projectSearchText := strings.ToLower(record.Name + " " + record.WorkspacePath)
		bodyMatches := make([]string, len(metas))
		searchBody := make([]bool, len(metas))
		if normalizedQuery != "" && !strings.Contains(projectSearchText, normalizedQuery) {
			store, err := service.projectSessionStore(record.ID, layout.SessionsDir(), false)
			if err != nil {
				return HistoryPage{}, err
			}
			group, searchContext := errgroup.WithContext(ctx)
			// Independent journals can be read concurrently, with a fixed I/O
			// budget rather than one goroutine per saved conversation.
			group.SetLimit(8)
			for index, meta := range metas {
				if strings.Contains(strings.ToLower(meta.Title+" "+meta.ID), normalizedQuery) {
					continue
				}
				searchBody[index] = true
				group.Go(func() (err error) {
					defer func() {
						if recovered := recover(); recovered != nil {
							err = fmt.Errorf("history search panic for %s: %v", meta.ID, recovered)
							slog.ErrorContext(searchContext, "History search failed", "session_id", meta.ID, "err", err)
						}
					}()
					bodyMatches[index], err = store.SearchHistoryContent(searchContext, meta.ID, normalizedQuery)
					return err
				})
			}
			if err := group.Wait(); err != nil {
				return HistoryPage{}, err
			}
		}
		for index, meta := range metas {
			if err := ctx.Err(); err != nil {
				return HistoryPage{}, err
			}
			excerpt := bodyMatches[index]
			if searchBody[index] && excerpt == "" {
				continue
			}
			items = append(items, HistoryItem{
				ProjectID: record.ID, ProjectName: record.Name,
				Session: sessionFromMeta(meta, record.ID, runningBindings),
				Excerpt: excerpt,
			})
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		if !items[i].Session.UpdatedAt.Equal(items[j].Session.UpdatedAt) {
			return items[i].Session.UpdatedAt.After(items[j].Session.UpdatedAt)
		}
		if items[i].ProjectID != items[j].ProjectID {
			return items[i].ProjectID < items[j].ProjectID
		}
		return items[i].Session.ID < items[j].Session.ID
	})
	page.Total = len(items)
	if page.Offset >= page.Total {
		return page, nil
	}
	end := min(page.Offset+query.Limit, page.Total)
	page.Items = append(page.Items, items[page.Offset:end]...)
	page.HasMore = end < page.Total
	slog.DebugContext(context.Background(), fmt.Sprintf(
		"[app/agentchat] searched conversation history query_length=%d total=%d offset=%d returned=%d duration=%s",
		len([]rune(normalizedQuery)), page.Total, page.Offset, len(page.Items), time.Since(startedAt),
	))
	return page, nil
}

func (service *Service) readProjectSessions(projectID, sessionsDir string) ([]session.SessionMeta, error) {
	store, err := service.projectSessionStore(projectID, sessionsDir, false)
	if err != nil {
		return nil, err
	}
	metas, err := store.List("")
	if err != nil {
		return nil, err
	}
	visible := make([]session.SessionMeta, 0, len(metas))
	for _, meta := range metas {
		if !agentconversation.IsReservedSessionID(meta.ID) {
			visible = append(visible, meta)
		}
	}
	return visible, nil
}

func (service *Service) CreateSession(projectID, title string, customAgentID *string) (Session, error) {
	binding, err := service.ResolveBinding(Binding{ProjectID: projectID, SessionID: "draft"})
	if err != nil {
		return Session{}, err
	}
	project, err := service.projectRuntime(context.Background(), binding.ProjectID)
	if err != nil {
		return Session{}, err
	}
	runtimeCfg, err := refreshRuntimeConfig(project)
	if err != nil {
		return Session{}, err
	}
	var seed conversationconfig.Config
	if customAgentID == nil {
		seed, err = agentconversation.RecentSessionSeed(project.store, &runtimeCfg, binding.agentKind, "")
	} else {
		seed, err = conversationconfig.DefaultWithCustomAgent(&runtimeCfg, binding.agentKind, *customAgentID)
	}
	if err != nil {
		return Session{}, err
	}
	sess, err := project.store.CreateWithRuntimeConfig(title, seed)
	if err != nil {
		return Session{}, err
	}
	slog.InfoContext(context.Background(), fmt.Sprintf(
		"[app/agentchat] created project session project_id=%s workspace=%q session_id=%s custom_agent_id=%q",
		binding.ProjectID, binding.Workspace, sess.ID, seed.CustomAgentID,
	))
	return Session{
		ID: sess.ID, Title: sess.Title(), CreatedAt: sess.CreatedAt, UpdatedAt: sess.UpdatedAt,
		MessageCount: sess.MessageCount(), CustomAgentID: seed.CustomAgentID,
	}, nil
}

func (service *Service) RenameSession(projectID, sessionID, title string) error {
	binding, err := service.ResolveBinding(Binding{ProjectID: projectID, SessionID: sessionID})
	if err != nil {
		return err
	}
	project, err := service.projectRuntime(context.Background(), binding.ProjectID)
	if err != nil {
		return err
	}
	return project.store.Rename(binding.SessionID, title)
}

// DeleteSession refuses to remove a running conversation and never re-points
// the foreground Writing Session.
func (service *Service) DeleteSession(projectID, sessionID string) error {
	service.admission.Lock()
	defer service.admission.Unlock()
	ctx := context.Background()
	binding, err := service.ResolveBinding(Binding{ProjectID: projectID, SessionID: sessionID})
	if err != nil {
		return err
	}
	if err := service.requireIdle(binding); err != nil {
		return err
	}
	project, err := service.projectRuntime(ctx, binding.ProjectID)
	if err != nil {
		return err
	}
	if err := project.store.CheckDelete(binding.SessionID); err != nil {
		return err
	}
	roots, err := project.executionRuntime.ProjectSessionBindings(ctx, binding.ProjectID, binding.SessionID)
	if err != nil {
		return err
	}
	if err := project.executionRuntime.CloseProjectSessionBindings(ctx, binding.ProjectID, binding.SessionID); err != nil {
		return err
	}
	// The product journal is the deletion commit point. Never tombstone embedded
	// Agent state or remove children before this durable conversation is removed.
	err = project.store.Delete(binding.SessionID)
	if err != nil && !errors.Is(err, session.ErrDeletionCleanup) {
		return err
	}
	for _, root := range roots {
		err = errors.Join(err, project.executionRuntime.DeleteSessionChildren(ctx, root))
	}
	service.starts.ReleaseScope(binding.ProjectID, binding.SessionID)
	if err != nil {
		slog.ErrorContext(ctx, "[app/agentchat] conversation deleted with cleanup errors", "project_id", binding.ProjectID, "session_id", binding.SessionID, "error", err)
	}
	return nil
}
