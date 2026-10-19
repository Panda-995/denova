package builtin

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"sort"
	"strings"
	"sync"

	"github.com/alfredxw/denova/agent"
	agentevent "github.com/alfredxw/denova/agent/lifecycle/event"
	agentinteraction "github.com/alfredxw/denova/agent/lifecycle/interaction"
	agentschema "github.com/alfredxw/denova/agent/schema"
	agentsession "github.com/alfredxw/denova/agent/session"
)

// SessionOpener is the narrow non-owning lifecycle seam needed by local
// delegation. agent.Agent implements it; an Executor never closes the owner.
type SessionOpener interface {
	Session(context.Context, agentsession.Key) (*agent.Session, error)
	ListSessions(context.Context, agentsession.Selector) ([]agentsession.Key, error)
}

type exactSessionCatalog interface {
	SessionExists(context.Context, agentsession.Key) (bool, error)
}

type activeSessionCounter interface {
	CountActiveSessions(context.Context, agentsession.Selector) (int, error)
}

type LocalTaskOptions struct {
	Parallelism      int
	CompletionParent *agent.Session
	MaxResultBytes   int
	// Self identifies the caller for scoped instance discovery; it has no Run.
	Self TaskRef
}

// LocalTaskAgent binds one stable selector to an Agent owner. Different
// selectors may share one owner or use independent Sources/Stores.
type LocalTaskAgent struct {
	Name        string
	Description string
	Opener      SessionOpener
	Identity    agentschema.CapabilityIdentity
	// Attributes contain the stable parent Session and definition selector.
	// Per-Run ownership belongs in HostData and never changes Session identity.
	Attributes map[string]string
	// HostData binds Start/FollowUp to this executor's current host Run. Queue,
	// Steer, and Resume retain the target Run's previously accepted binding.
	HostData *agent.HostData
	// LookupAttributes are the parent-stable subset used to find the exact
	// durable child key again. Per-turn route attributes deliberately stay out
	// of this selector, so an earlier TaskRef remains usable from later parent
	// cycles and after a cold restart.
	LookupAttributes map[string]string
}

// LocalTasks executes named children through Agent -> Session -> Run. The
// executor keeps no authoritative task map: TaskRef is sufficient to reopen a
// Session and attach a Run after a cold restart.
type LocalTasks struct {
	agents           map[string]LocalTaskAgent
	ordered          []TaskAgentInfo
	parallelism      int
	identity         agentschema.CapabilityIdentity
	completionParent *agent.Session
	maxResultBytes   int
	self             TaskRef
	watchMu          sync.Mutex
	watched          map[string]*agent.Run
}

// Admission spans the count and accepted Run across all executor instances.
// This mutex holds no task state; authoritative activity stays with Sessions.
var localTaskAdmissionMu sync.Mutex

func NewLocalTasks(options LocalTaskOptions, candidates ...LocalTaskAgent) (*LocalTasks, error) {
	if len(candidates) == 0 {
		return nil, errors.New("local tasks require at least one Agent")
	}
	if options.Parallelism <= 0 {
		return nil, errors.New("local tasks require positive parallelism")
	}
	if options.MaxResultBytes <= 0 {
		options.MaxResultBytes = defaultResultBytes
	}
	resolved := make(map[string]LocalTaskAgent, len(candidates))
	identities := make([]struct {
		Name, Description string
		Identity          agentschema.CapabilityIdentity
	}, 0, len(candidates))
	for index, candidate := range candidates {
		candidate.Name = strings.TrimSpace(candidate.Name)
		candidate.Description = strings.TrimSpace(candidate.Description)
		if candidate.Name == "" || candidate.Opener == nil || candidate.Identity.Kind == "" || candidate.Identity.Version == 0 {
			return nil, fmt.Errorf("local task Agent %d requires name, opener, and stable identity", index)
		}
		if _, duplicate := resolved[candidate.Name]; duplicate {
			return nil, fmt.Errorf("local task Agent %q is duplicated", candidate.Name)
		}
		if options.CompletionParent != nil {
			parentAttributes, err := agent.ChildSessionAttributes(options.CompletionParent.Key())
			if err != nil {
				return nil, err
			}
			candidate.Attributes = cloneTaskAttributes(candidate.Attributes)
			candidate.LookupAttributes = cloneTaskAttributes(candidate.LookupAttributes)
			if candidate.Attributes == nil {
				candidate.Attributes = make(map[string]string)
			}
			if candidate.LookupAttributes == nil {
				candidate.LookupAttributes = make(map[string]string)
			}
			for name, value := range parentAttributes {
				if existing := candidate.Attributes[name]; existing != "" && existing != value {
					return nil, fmt.Errorf("child Agent parent identity differs from completion parent")
				}
				candidate.Attributes[name], candidate.LookupAttributes[name] = value, value
			}
		}
		candidate.Attributes = cloneTaskAttributes(candidate.Attributes)
		candidate.HostData = cloneTaskHostData(candidate.HostData)
		candidate.LookupAttributes = cloneTaskAttributes(candidate.LookupAttributes)
		for name, value := range candidate.LookupAttributes {
			if candidate.Attributes[name] != value {
				return nil, fmt.Errorf("local task Agent %q lookup attribute %q is not immutable", candidate.Name, name)
			}
		}
		resolved[candidate.Name] = candidate
		identities = append(identities, struct {
			Name, Description string
			Identity          agentschema.CapabilityIdentity
		}{candidate.Name, candidate.Description, candidate.Identity})
	}
	sort.Slice(identities, func(i, j int) bool { return identities[i].Name < identities[j].Name })
	ordered := make([]TaskAgentInfo, len(identities))
	for index, candidate := range identities {
		ordered[index] = TaskAgentInfo{Name: candidate.Name, Description: candidate.Description}
	}
	return &LocalTasks{
		agents: resolved, ordered: ordered, parallelism: options.Parallelism, self: options.Self,
		completionParent: options.CompletionParent, maxResultBytes: options.MaxResultBytes,
		watched: make(map[string]*agent.Run),
		identity: toolsetIdentity("tasks.local", struct {
			Parallelism    int
			MaxResultBytes int
			Agents         any
		}{options.Parallelism, options.MaxResultBytes, identities}),
	}, nil
}

func (tasks *LocalTasks) Identity() agentschema.CapabilityIdentity {
	if tasks == nil {
		return agentschema.CapabilityIdentity{}
	}
	return tasks.identity
}

func (tasks *LocalTasks) ResultLimit() int { return tasks.maxResultBytes }

func (tasks *LocalTasks) Start(ctx context.Context, request TaskRequest) (Task, error) {
	candidate, err := tasks.agent(request.Agent)
	if err != nil {
		return Task{}, err
	}
	prompt := strings.TrimSpace(request.Prompt)
	if prompt == "" {
		return Task{}, errors.New("task prompt is required")
	}
	commandID := strings.TrimSpace(request.IdempotencyKey)
	if commandID == "" {
		commandID, err = localTaskID("command")
		if err != nil {
			return Task{}, err
		}
	}
	sessionID := localTaskSessionID(candidate.Name, commandID)

	localTaskAdmissionMu.Lock()
	defer localTaskAdmissionMu.Unlock()
	if existing, found, existingErr := tasks.existingTask(ctx, candidate, sessionID, commandID, prompt); existingErr != nil {
		return Task{}, existingErr
	} else if found {
		if completionErr := tasks.resumeCompletionTracking(ctx, existing); completionErr != nil {
			return existing, completionErr
		}
		return existing, nil
	}
	if err := tasks.checkCapacity(ctx); err != nil {
		return Task{}, err
	}
	key := localTaskSessionKey(candidate, sessionID)
	if tasks.completionParent != nil {
		if err := tasks.completionParent.RegisterChildTask(ctx, agent.ChildTask{Key: key}); err != nil {
			return Task{}, err
		}
	}
	session, err := candidate.Opener.Session(ctx, key)
	if err != nil {
		return Task{}, fmt.Errorf("open task Session: %w", err)
	}
	run, err := session.Run(ctx, agent.Input{Text: prompt, IdempotencyKey: commandID, HostData: cloneTaskHostData(candidate.HostData)})
	if err != nil {
		if errors.Is(err, agentsession.ErrCommitUnknown) {
			return Task{}, fmt.Errorf("task Run admission is uncertain: %w", err)
		}
		// A rejected call can still refer to accepted work after a failed
		// attachment. Only remove an empty, authoritatively unaccepted child.
		if tasks.completionParent != nil {
			_, discardErr := tasks.completionParent.DiscardRejectedChildTask(context.Background(), session)
			return Task{}, errors.Join(fmt.Errorf("start task Run: %w", err), discardErr)
		}
		snapshot, inspectErr := session.Snapshot(context.Background())
		if inspectErr != nil || snapshot.ActiveRunID != "" || len(snapshot.QueuedRuns) != 0 || len(snapshot.RecentRuns) != 0 {
			return Task{}, errors.Join(fmt.Errorf("start task Run: %w", err), inspectErr)
		}
		if deleteErr := session.Delete(context.Background()); deleteErr != nil {
			return Task{}, errors.Join(
				fmt.Errorf("start task Run: %w", err),
				fmt.Errorf("delete rejected task Session: %w", deleteErr),
			)
		}
		return Task{}, fmt.Errorf("start task Run: %w", err)
	}
	ref := TaskRef{Agent: candidate.Name, Session: sessionID, Run: run.ID()}
	receipt := run.Receipt()
	accepted := Task{Ref: ref, Status: "running", Receipt: &receipt}
	if completionErr := tasks.trackTaskCompletion(ctx, ref); completionErr != nil {
		// The durable intent still names this child. Recovery can repair the lost
		// parent receipt without cancelling already accepted work.
		return accepted, completionErr
	}
	tasks.watchCompletion(ctx, run, ref)
	return accepted, nil
}

func (tasks *LocalTasks) Observe(ctx context.Context, ref TaskRef, cursor string) (TaskObservation, error) {
	return retryClosedTaskRead(ctx, func() (TaskObservation, error) {
		return tasks.observe(ctx, ref, cursor)
	})
}

func (tasks *LocalTasks) observe(ctx context.Context, ref TaskRef, cursor string) (TaskObservation, error) {
	_, session, err := tasks.open(ctx, ref)
	if err != nil {
		return TaskObservation{}, err
	}
	after, err := parseTaskCursor(cursor)
	if err != nil {
		return TaskObservation{}, err
	}
	observation, err := session.Observe(ctx, after)
	if err != nil {
		return TaskObservation{}, err
	}
	task, err := tasks.taskFromSessionSnapshot(ctx, session, ref, observation.Snapshot)
	if err != nil {
		return TaskObservation{}, err
	}
	result := TaskObservation{Task: task}
	result.Events, result.Cursor, result.Output, result.Incomplete, err = collectTaskEvents(ctx, observation, ref.Run, after)
	if result.Output == "" {
		result.Output = taskSnapshotOutput(observation.Snapshot, ref.Run)
	}
	terminal := isTaskTerminal(result.Task.Status)
	if err == nil && result.Output == "" && terminal {
		var replayIncomplete bool
		result.Output, replayIncomplete, err = replayTaskFinal(ctx, session, ref.Run, observation.Snapshot.RetentionStart)
		result.Incomplete = result.Incomplete || replayIncomplete
	}
	if result.Output != "" {
		result.Task.Output = result.Output
	}
	if terminal && observation.Snapshot.ActiveRunID == "" && len(observation.Snapshot.QueuedRuns) == 0 {
		err = errors.Join(err, session.Close(context.Background()))
	}
	return result, err
}

func (tasks *LocalTasks) Steer(ctx context.Context, ref TaskRef, input agent.Input) (agentevent.CommandReceipt, error) {
	_, session, err := tasks.open(ctx, ref)
	if err != nil {
		return agentevent.CommandReceipt{}, err
	}
	run, found, err := session.AttachRun(ctx, ref.Run)
	if err != nil || !found {
		if err == nil {
			err = ErrTaskNotFound
		}
		return agentevent.CommandReceipt{}, err
	}
	original, _, err := session.RunInput(ctx, ref.Run)
	if err != nil {
		return agentevent.CommandReceipt{}, err
	}
	input.HostData = original.HostData
	return run.Steer(ctx, input)
}

func (tasks *LocalTasks) Respond(
	ctx context.Context,
	ref TaskRef,
	interactionID string,
	response agentinteraction.InteractionResponse,
) error {
	_, session, err := tasks.open(ctx, ref)
	if err != nil {
		return err
	}
	run, found, err := session.AttachRun(ctx, ref.Run)
	if err != nil || !found {
		if err == nil {
			err = ErrTaskNotFound
		}
		return err
	}
	return run.Respond(ctx, strings.TrimSpace(interactionID), response)
}

func (tasks *LocalTasks) Abort(ctx context.Context, ref TaskRef, request agentevent.AbortRequest) (agentevent.CommandReceipt, error) {
	_, session, err := tasks.open(ctx, ref)
	if err != nil {
		return agentevent.CommandReceipt{}, err
	}
	run, found, err := session.AttachRun(ctx, ref.Run)
	if err != nil || !found {
		if err == nil {
			err = ErrTaskNotFound
		}
		return agentevent.CommandReceipt{}, err
	}
	return run.Abort(ctx, request)
}

func (tasks *LocalTasks) existingTask(
	ctx context.Context,
	candidate LocalTaskAgent,
	sessionID string,
	commandID string,
	prompt string,
) (Task, bool, error) {
	keys, err := taskSessionKeys(ctx, candidate, sessionID)
	if err != nil {
		return Task{}, false, err
	}
	switch len(keys) {
	case 0:
		return Task{}, false, nil
	case 1:
	default:
		return Task{}, false, errors.New("task Session identity is ambiguous")
	}
	session, err := candidate.Opener.Session(ctx, keys[0])
	if err != nil {
		return Task{}, false, err
	}
	exact, found, err := session.CommandSnapshot(ctx, commandID)
	if err != nil {
		return Task{}, false, err
	}
	if !found {
		snapshot, err := session.Snapshot(ctx)
		if err != nil {
			return Task{}, false, err
		}
		if snapshot.ActiveRunID == "" && len(snapshot.QueuedRuns) == 0 && len(snapshot.RecentRuns) == 0 {
			return Task{}, false, nil
		}
		return Task{}, false, errors.New("task Session idempotency identity is inconsistent")
	}
	ref := TaskRef{Agent: candidate.Name, Session: sessionID, Run: exact.Receipt.RunID}
	original, found, err := session.RunInput(ctx, ref.Run)
	if err != nil {
		return Task{}, false, err
	}
	if !found || original.Text != prompt {
		return Task{}, false, agent.ErrIdempotencyConflict
	}
	snapshot, err := session.Snapshot(ctx)
	if err != nil {
		return Task{}, false, err
	}
	task, taskErr := tasks.taskFromSessionSnapshot(ctx, session, ref, snapshot)
	if exact.Result != nil && snapshot.ActiveRunID == "" && len(snapshot.QueuedRuns) == 0 {
		if closeErr := session.Close(context.Background()); closeErr != nil {
			taskErr = errors.Join(taskErr, closeErr)
		}
	}
	return task, true, taskErr
}

func (tasks *LocalTasks) activeTaskCount(ctx context.Context) (int, error) {
	total := 0
	for _, candidate := range tasks.agents {
		selector := taskSessionSelector(candidate, "")
		if counter, ok := candidate.Opener.(activeSessionCounter); ok {
			count, err := counter.CountActiveSessions(ctx, selector)
			if err != nil {
				return 0, err
			}
			total += count
			continue
		}
		keys, err := candidate.Opener.ListSessions(ctx, selector)
		if err != nil {
			return 0, err
		}
		for _, key := range keys {
			session, openErr := candidate.Opener.Session(ctx, key)
			if openErr != nil {
				return 0, openErr
			}
			snapshot, snapshotErr := session.Snapshot(ctx)
			if snapshotErr != nil {
				return 0, snapshotErr
			}
			if snapshot.ActiveRunID != "" && snapshot.ActiveStatus != agentschema.ResultSuspended {
				total++
			} else if snapshot.ActiveRunID == "" {
				if closeErr := session.Close(context.Background()); closeErr != nil {
					return 0, closeErr
				}
			}
		}
	}
	return total, nil
}

func (tasks *LocalTasks) open(ctx context.Context, ref TaskRef) (LocalTaskAgent, *agent.Session, error) {
	candidate, err := tasks.agent(ref.Agent)
	if err != nil {
		return LocalTaskAgent{}, nil, err
	}
	if strings.TrimSpace(ref.Session) == "" || strings.TrimSpace(ref.Run) == "" {
		return LocalTaskAgent{}, nil, errors.New("task ref requires agent, session, and run")
	}
	session, err := tasks.openExisting(ctx, candidate, ref.Session)
	return candidate, session, err
}

func (tasks *LocalTasks) openExisting(
	ctx context.Context,
	candidate LocalTaskAgent,
	sessionID string,
) (*agent.Session, error) {
	if strings.TrimSpace(sessionID) == "" {
		return nil, errors.New("task Session ID is required")
	}
	keys, err := taskSessionKeys(ctx, candidate, sessionID)
	if err != nil {
		return nil, err
	}
	if len(keys) == 0 {
		return nil, ErrTaskNotFound
	}
	if len(keys) != 1 {
		return nil, errors.New("task Session identity is ambiguous")
	}
	return candidate.Opener.Session(ctx, keys[0])
}

func taskSessionKeys(
	ctx context.Context,
	candidate LocalTaskAgent,
	sessionID string,
) ([]agentsession.Key, error) {
	if exact, ok := candidate.Opener.(exactSessionCatalog); ok && sessionID != "" && maps.Equal(candidate.Attributes, candidate.LookupAttributes) {
		key := localTaskSessionKey(candidate, sessionID)
		found, err := exact.SessionExists(ctx, key)
		if err != nil || !found {
			return nil, err
		}
		return []agentsession.Key{key}, nil
	}
	return candidate.Opener.ListSessions(ctx, taskSessionSelector(candidate, sessionID))
}

func taskSessionSelector(candidate LocalTaskAgent, sessionID string) agentsession.Selector {
	attributes := cloneTaskAttributes(candidate.LookupAttributes)
	if attributes == nil {
		attributes = make(map[string]string, 1)
	}
	attributes["agent"] = candidate.Name
	return agentsession.Selector{
		Namespace:  "task." + candidate.Name,
		ID:         sessionID,
		Attributes: attributes,
	}
}

func (tasks *LocalTasks) agent(name string) (LocalTaskAgent, error) {
	if tasks == nil {
		return LocalTaskAgent{}, errors.New("local tasks are unavailable")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = DefaultTaskAgentName
	}
	candidate, ok := tasks.agents[name]
	if !ok {
		return LocalTaskAgent{}, fmt.Errorf("%w: Agent %q", ErrTaskNotFound, name)
	}
	return candidate, nil
}

func localTaskSessionID(agentName, commandID string) string {
	return toolsetIdentity("task.session", struct{ Agent, Command string }{agentName, commandID}).ConfigHash[:32]
}

func localTaskID(prefix string) (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("generate task id: %w", err)
	}
	return prefix + "-" + hex.EncodeToString(value[:]), nil
}

func localTaskSessionKey(candidate LocalTaskAgent, sessionID string) agentsession.Key {
	attributes := cloneTaskAttributes(candidate.Attributes)
	if attributes == nil {
		attributes = make(map[string]string, 1)
	}
	attributes["agent"] = candidate.Name
	return agentsession.Key{Namespace: "task." + candidate.Name, ID: sessionID, Attributes: attributes}
}

func cloneTaskAttributes(source map[string]string) map[string]string {
	if source == nil {
		return nil
	}
	cloned := make(map[string]string, len(source))
	for name, value := range source {
		cloned[name] = value
	}
	return cloned
}

func cloneTaskHostData(data *agent.HostData) *agent.HostData {
	if data == nil {
		return nil
	}
	return &agent.HostData{Type: data.Type, Version: data.Version, Data: append(json.RawMessage(nil), data.Data...)}
}

var _ TaskExecutor = (*LocalTasks)(nil)
