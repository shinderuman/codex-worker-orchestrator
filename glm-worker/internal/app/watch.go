package app

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type watchOptions struct {
	verbose        bool
	followInterval time.Duration
	statusInterval time.Duration
	changeInterval time.Duration
	now            func() time.Time
	stop           <-chan struct{}

	openEventLog func(path string) (*os.File, error)
	statEventLog func(path string) (os.FileInfo, error)
	probeLock    func(path string) LockProbe
	acquireLease func(path string) (*repoLockLease, error)
}

type watchStartEvent struct {
	Type           string  `json:"type"`
	TaskID         *string `json:"task_id"`
	EventLog       *string `json:"event_log"`
	EventLogStatus string  `json:"event_log_status"`
}

type watchLogStatusEvent struct {
	Type   string `json:"type"`
	Status string `json:"status"`
}

type watchExitEvent struct {
	Type      string  `json:"type"`
	TaskID    string  `json:"task_id"`
	Status    string  `json:"status"`
	NewTaskID *string `json:"new_task_id,omitempty"`
}

type watchOrphanExitEvent struct {
	Type           string                         `json:"type"`
	TaskID         string                         `json:"task_id"`
	Status         string                         `json:"status"`
	Consistent     bool                           `json:"consistent"`
	Inconsistency  *string                        `json:"inconsistency"`
	RequiredAction *string                        `json:"required_action"`
	AllowedActions []string                       `json:"allowed_actions"`
	ResumeKind     *string                        `json:"resume_kind"`
	LastMaterial   *parentHandoffRecoveryMaterial `json:"last_material"`
	ArtifactDir    *string                        `json:"artifact_dir"`
}

const defaultWatchFollowInterval = 500 * time.Millisecond

const watchStatusOrphanTerminal = "orphan-terminal"

func (o watchOptions) openLog(path string) (*os.File, error) {
	if o.openEventLog != nil {
		return o.openEventLog(path)
	}
	return os.Open(path)
}

func (o watchOptions) statLog(path string) (os.FileInfo, error) {
	if o.statEventLog != nil {
		return o.statEventLog(path)
	}
	return os.Stat(path)
}

func (o watchOptions) probe(path string) LockProbe {
	if o.probeLock != nil {
		return o.probeLock(path)
	}
	return ProbeRepoLock(path)
}

func (o watchOptions) lease(path string) (*repoLockLease, error) {
	if o.acquireLease != nil {
		return o.acquireLease(path)
	}
	return AcquireRepoLockLease(path)
}

func defaultWatchOptions(verbose bool) watchOptions {
	return watchOptions{
		verbose:        verbose,
		followInterval: defaultWatchFollowInterval,
		statusInterval: defaultWatchStatusInterval,
		changeInterval: defaultWatchChangeInterval,
		now:            time.Now,
	}
}

func printWatch(st *state.StateStore, stdout io.Writer, opts watchOptions) error {
	taskID := st.ReadOr("task.id", "")
	if taskID == "" {
		return writeWatchEvent(stdout, watchStartEvent{
			Type: "watch_start", TaskID: nil, EventLog: nil, EventLogStatus: statusNone,
		})
	}
	path := st.TaskEventLogPath(taskID)
	file, err := opts.openLog(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("event log %sを開けません: %w", path, err)
		}
		return writeWatchEvent(stdout, watchStartEvent{
			Type: "watch_start", TaskID: &taskID, EventLog: &path, EventLogStatus: "empty",
		})
	}
	defer func() { _ = file.Close() }()
	if err := writeWatchEvent(stdout, watchStartEvent{
		Type: "watch_start", TaskID: &taskID, EventLog: &path, EventLogStatus: "following",
	}); err != nil {
		return err
	}
	return watchTaskEvents(st, taskID, file, path, stdout, opts)
}

func watchTerminal(st *state.StateStore, taskID string, stdout io.Writer, opts watchOptions) (bool, error) {
	terminal, err := watchTaskTerminalState(st, taskID, stdout)
	if terminal || err != nil {
		return terminal, err
	}
	return watchOrphanTerminal(st, taskID, stdout, opts)
}

func watchTaskTerminalState(st *state.StateStore, taskID string, stdout io.Writer) (bool, error) {
	current := st.ReadOr("task.id", "")
	if current != "" && current != taskID {
		return true, writeWatchEvent(stdout, watchExitEvent{
			Type: "watch_exit", TaskID: taskID, Status: "task-switched", NewTaskID: &current,
		})
	}
	if status := st.TaskStatus(); status != state.TaskStatusActive {
		return true, writeWatchEvent(stdout, watchExitEvent{Type: "watch_exit", TaskID: taskID, Status: string(status)})
	}
	return false, nil
}

func watchOrphanTerminal(st *state.StateStore, taskID string, stdout io.Writer, opts watchOptions) (bool, error) {
	if opts.probe(st.LockPath()).State != LockFree {
		return false, nil
	}
	material := watchOrphanMaterial(st, taskID)
	if material == nil || material.Outcome != modelCallOutcomeError {
		return false, nil
	}
	lease, err := opts.lease(st.LockPath())
	if err != nil {
		return false, nil
	}
	defer lease.Release()
	terminal, err := watchTaskTerminalState(st, taskID, stdout)
	if terminal || err != nil {
		return terminal, err
	}
	material = watchOrphanMaterial(st, taskID)
	if material == nil || material.Outcome != modelCallOutcomeError {
		return false, nil
	}
	return true, writeWatchEvent(stdout, buildWatchOrphanExitEvent(st, taskID, *material))
}

func watchOrphanMaterial(st *state.StateStore, taskID string) *state.ModelCallLog {
	logs, err := readStatusTelemetry(st, taskID)
	if err != nil {
		return nil
	}
	return lastParentActionMaterial(logs)
}

func buildWatchOrphanExitEvent(st *state.StateStore, taskID string, log state.ModelCallLog) watchOrphanExitEvent {
	event := watchOrphanExitEvent{
		Type:           "watch_exit",
		TaskID:         taskID,
		Status:         watchStatusOrphanTerminal,
		Consistent:     true,
		AllowedActions: []string{},
		LastMaterial:   recoveryMaterialFromHandoff(parentHandoffMaterialFromLog(log)),
		ArtifactDir:    stringPtr(st.ArtifactDir(taskID)),
	}
	plan, err := st.ParentActionPlan()
	if err != nil {
		event.Consistent = false
		event.Inconsistency = stringPtr(err.Error())
		return event
	}
	required := string(plan.RequiredAction)
	event.RequiredAction = &required
	for _, action := range plan.AllowedActions {
		event.AllowedActions = append(event.AllowedActions, string(action))
	}
	event.ResumeKind = stringPtr(plan.ResumeKind)
	return event
}

func watchTaskEvents(st *state.StateStore, taskID string, file *os.File, path string, stdout io.Writer, opts watchOptions) error {
	pending, status, terminal, err := prepareWatchTaskEvents(st, taskID, file, stdout, opts)
	if err != nil {
		return err
	}
	if terminal {
		return nil
	}
	for {
		var done bool
		pending, done, err = watchTaskTick(st, taskID, file, path, stdout, pending, &status, opts)
		if err != nil {
			return err
		}
		if done {
			return nil
		}
	}
}

func watchTaskTick(st *state.StateStore, taskID string, file *os.File, path string, stdout io.Writer, pending []byte, status *watchLiveStatus, opts watchOptions) ([]byte, bool, error) {
	if waitWatchTick(opts.stop, opts.followInterval) {
		return pending, true, nil
	}
	removed, err := watchEventLogRemoved(path, opts)
	if err != nil {
		return pending, false, err
	}
	if removed {
		return pending, true, writeWatchEvent(stdout, watchLogStatusEvent{Type: "event_log_status", Status: "removed"})
	}
	pending, err = drainTaskEvents(file, stdout, pending, status.tracker.observe)
	if err != nil {
		return pending, false, err
	}
	if err := status.refresh(false); err != nil {
		return pending, false, err
	}
	terminal, err := watchTerminal(st, taskID, stdout, opts)
	if err != nil {
		return pending, false, err
	}
	return pending, terminal, nil
}

func prepareWatchTaskEvents(st *state.StateStore, taskID string, file *os.File, stdout io.Writer, opts watchOptions) ([]byte, watchLiveStatus, bool, error) {
	tracker := newWatchToolTracker()
	pending, err := drainTaskEvents(file, stdout, nil, tracker.observe)
	status := watchLiveStatus{st: st, taskID: taskID, stdout: stdout, tracker: tracker, opts: opts}
	if err != nil {
		return nil, status, false, err
	}
	if err := status.refresh(true); err != nil {
		return nil, status, false, err
	}
	terminal, err := watchTerminal(st, taskID, stdout, opts)
	return pending, status, terminal, err
}

func waitWatchTick(stop <-chan struct{}, interval time.Duration) bool {
	select {
	case <-stop:
		return true
	case <-time.After(interval):
		return false
	}
}

func watchEventLogRemoved(path string, opts watchOptions) (bool, error) {
	if _, err := opts.statLog(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return true, nil
		}
		return false, fmt.Errorf("event log %sの状態を取得できません: %w", path, err)
	}
	return false, nil
}

func writeWatchEvent(w io.Writer, event any) error {
	line, err := marshalEventLine(event)
	if err != nil {
		return err
	}
	_, err = w.Write(line)
	return err
}

func drainTaskEvents(file *os.File, stdout io.Writer, pending []byte, onRecord func(state.TaskEventRecord)) ([]byte, error) {
	buffer := make([]byte, 32*1024)
	for {
		read, err := file.Read(buffer)
		if read > 0 {
			pending = append(pending, buffer[:read]...)
			pending, err = emitCompleteTaskEventLines(pending, stdout, onRecord)
			if err != nil {
				return pending, err
			}
		}
		if errors.Is(err, io.EOF) {
			return pending, nil
		}
		if err != nil {
			return pending, err
		}
	}
}

func emitCompleteTaskEventLines(pending []byte, stdout io.Writer, onRecord func(state.TaskEventRecord)) ([]byte, error) {
	for {
		index := bytes.IndexByte(pending, '\n')
		if index < 0 {
			return pending, nil
		}
		if err := emitTaskEventLine(pending[:index], stdout, onRecord); err != nil {
			return pending, err
		}
		pending = pending[index+1:]
	}
}

func emitTaskEventLine(line []byte, stdout io.Writer, onRecord func(state.TaskEventRecord)) error {
	trimmed := bytes.TrimSpace(line)
	if len(trimmed) == 0 {
		return nil
	}
	record, err := state.ParseTaskEventLine(trimmed)
	if err != nil {
		return writeWatchEvent(stdout, eventLogSkippedLine{Type: "event_skipped", Error: err.Error()})
	}
	if onRecord != nil {
		onRecord(record)
	}
	_, writeErr := stdout.Write(append(bytes.Clone(trimmed), '\n'))
	return writeErr
}
