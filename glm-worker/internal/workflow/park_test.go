package workflow

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"

	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type parkFixture struct {
	repo     string
	st       *state.StateStore
	w        *Workflow
	worktree string
}

func newParkFixture(t *testing.T) *parkFixture {
	t.Helper()
	repo := newRetentionGitRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "parked-dirty.md"), []byte("parked task working set\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stateBase := t.TempDir()
	st, err := state.NewStateStore(config.AppConfig{
		StateBase: stateBase,
		RepoHash:  "retentionhash",
		RepoRoot:  repo,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := state.CaptureGitBaseline(config.AppConfig{RepoRoot: repo}, st); err != nil {
		t.Fatal(err)
	}
	if err := st.Write("worker.id", "sess-park-worker"); err != nil {
		t.Fatal(err)
	}
	if err := st.Write("reviewer.id", "sess-park-reviewer"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusWaitingSolReview); err != nil {
		t.Fatal(err)
	}
	w := NewWorkflow(config.AppConfig{
		RepoRoot:     repo,
		StateBase:    stateBase,
		RepoHash:     "retentionhash",
		WorktreeBase: filepath.Join(t.TempDir(), "worktrees"),
		RepoShort:    "parkrepo",
	}, st, nil, io.Discard)
	w.now = newFakeClock().nowFunc
	return &parkFixture{repo: repo, st: st, w: w}
}

func (f *parkFixture) park(t *testing.T) parkOutput {
	t.Helper()
	var stdout bytes.Buffer
	if err := f.w.ExecutePark(&stdout); err != nil {
		t.Fatalf("ExecutePark: %v", err)
	}
	var output parkOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatalf("park output = %s: %v", stdout.String(), err)
	}
	f.worktree = output.Worktree
	return output
}

func TestParkPreservesTaskStateAndCreatesInterruptWorktree(t *testing.T) {
	fixture := newParkFixture(t)
	output := fixture.park(t)

	if output.Result != "parked" || output.FromStatus != string(state.TaskStatusWaitingSolReview) {
		t.Fatalf("park output = %#v", output)
	}
	if output.WorkerSession != "sess-park-worker" || output.ReviewerSession != "sess-park-reviewer" {
		t.Fatalf("park output loses sessions: %#v", output)
	}
	if fixture.st.TaskStatus() != state.TaskStatusParked {
		t.Fatalf("task status = %s, want parked", fixture.st.TaskStatus())
	}
	if fixture.st.ReadOr("worker.id", "") != "sess-park-worker" {
		t.Fatal("park cleared the worker session")
	}
	record, err := fixture.st.LoadParkRecord()
	if err != nil {
		t.Fatal(err)
	}
	if record.TaskID == "" || record.Head == "" || len(record.DirtyFiles) == 0 {
		t.Fatalf("park record = %#v", record)
	}
	if _, statErr := os.Stat(filepath.Join(fixture.worktree, "tracked.md")); statErr != nil {
		t.Fatalf("interrupt worktree missing tracked file: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(fixture.worktree, "parked-dirty.md")); !os.IsNotExist(statErr) {
		t.Fatal("interrupt worktree carries the parked task dirty file")
	}
	content, err := os.ReadFile(fixture.st.ParkContentPath("parked-dirty.md"))
	if err != nil || string(content) != "parked task working set\n" {
		t.Fatalf("park content artifact = %q err = %v", string(content), err)
	}
	origin, err := fixture.st.AttachSiblingStore(config.RepoHashFor(fixture.worktree)).LoadParkOrigin()
	if err != nil || origin.ParkID != record.ParkID || origin.TaskID != record.TaskID {
		t.Fatalf("park origin record = %#v err = %v", origin, err)
	}

	plan, err := fixture.st.ParentActionPlan()
	if err != nil {
		t.Fatal(err)
	}
	if plan.RequiredAction != state.ParentActionUnpark || !plan.Allows(state.ParentActionUnpark) {
		t.Fatalf("parked plan = %#v", plan)
	}
	_, admitted, err := fixture.st.AdmitNewTask()
	if err != nil || admitted {
		t.Fatalf("new task admitted while parked: admitted=%v err=%v", admitted, err)
	}

	var second bytes.Buffer
	if err := fixture.w.ExecutePark(&second); err != nil {
		t.Fatalf("idempotent park replay: %v", err)
	}
	var replay parkOutput
	if err := json.Unmarshal(second.Bytes(), &replay); err != nil || replay.ParkID != output.ParkID {
		t.Fatalf("park replay = %s err = %v", second.String(), err)
	}
}

func TestUnparkRestoresSameTaskAfterInterruptIntegration(t *testing.T) {
	fixture := newParkFixture(t)
	parkOutputValue := fixture.park(t)

	branch := parkOutputValue.Branch
	interruptChange := filepath.Join(fixture.worktree, "interrupt-feature.md")
	if err := os.WriteFile(interruptChange, []byte("interrupt task result\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runRetentionGit(t, fixture.worktree, "add", "interrupt-feature.md")
	runRetentionGit(t, fixture.worktree, "commit", "-q", "-m", "interrupt task")
	runRetentionGit(t, fixture.repo, "merge", "-q", "--ff-only", branch)

	var stdout bytes.Buffer
	if err := fixture.w.ExecuteUnpark(&stdout); err != nil {
		t.Fatalf("ExecuteUnpark: %v", err)
	}
	var output unparkOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatalf("unpark output = %s: %v", stdout.String(), err)
	}
	if output.Result != "unparked" || output.Integration != "interrupt-integrated" {
		t.Fatalf("unpark output = %#v", output)
	}
	if output.RestoredStatus != string(state.TaskStatusWaitingSolReview) {
		t.Fatalf("restored status = %s", output.RestoredStatus)
	}
	if fixture.st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("task status = %s", fixture.st.TaskStatus())
	}
	if fixture.st.ReadOr("worker.id", "") != "sess-park-worker" || fixture.st.ReadOr("reviewer.id", "") != "sess-park-reviewer" {
		t.Fatal("unpark lost the saved sessions")
	}
	if content, err := os.ReadFile(filepath.Join(fixture.repo, "parked-dirty.md")); err != nil || string(content) != "parked task working set\n" {
		t.Fatalf("parked working set changed: %q err = %v", string(content), err)
	}
	plan, err := fixture.st.ParentActionPlan()
	if err != nil {
		t.Fatal(err)
	}
	if plan.RequiredAction != state.ParentActionReview {
		t.Fatalf("restored plan = %#v", plan)
	}
}

func TestUnparkFailsClosedWhenParkedWorkingSetChanged(t *testing.T) {
	fixture := newParkFixture(t)
	fixture.park(t)

	if err := os.WriteFile(filepath.Join(fixture.repo, "parked-dirty.md"), []byte("tampered\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	err := fixture.w.ExecuteUnpark(&stdout)
	var workerErr *WorkerError
	if !errors.As(err, &workerErr) {
		t.Fatalf("unpark error = %T, want WorkerError: %v", err, err)
	}
	if !strings.Contains(workerErr.Message, "parked-dirty.md") {
		t.Fatalf("unpark error = %q", workerErr.Message)
	}
	if fixture.st.TaskStatus() != state.TaskStatusParked {
		t.Fatalf("task status = %s, want parked after failed unpark", fixture.st.TaskStatus())
	}
}

func TestUnparkFailsClosedOnUnprovenancedHeadMovement(t *testing.T) {
	fixture := newParkFixture(t)
	fixture.park(t)

	if err := os.WriteFile(filepath.Join(fixture.repo, "rogue.md"), []byte("rogue\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runRetentionGit(t, fixture.repo, "add", "rogue.md")
	runRetentionGit(t, fixture.repo, "commit", "-q", "-m", "rogue head movement")

	var stdout bytes.Buffer
	err := fixture.w.ExecuteUnpark(&stdout)
	var workerErr *WorkerError
	if !errors.As(err, &workerErr) {
		t.Fatalf("unpark error = %T, want WorkerError: %v", err, err)
	}
	if !strings.Contains(workerErr.Message, "親管理外file") {
		t.Fatalf("unpark error = %q", workerErr.Message)
	}
	if fixture.st.TaskStatus() != state.TaskStatusParked {
		t.Fatalf("task status = %s, want parked after failed unpark", fixture.st.TaskStatus())
	}
}

func TestUnparkAcceptsParentMetadataOnlyHeadMovement(t *testing.T) {
	fixture := newParkFixture(t)
	parked := fixture.park(t)

	if err := os.WriteFile(filepath.Join(fixture.repo, "IMPLEMENTATION_PLAN.local.md"), []byte("# Plan\n\n## ACTIVE\n\n- `IMPLEMENTATION_TASKS/interrupt.md`\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runRetentionGit(t, fixture.repo, "add", "IMPLEMENTATION_PLAN.local.md")
	runRetentionGit(t, fixture.repo, "commit", "-q", "-m", "parent metadata update")

	var stdout bytes.Buffer
	if err := fixture.w.ExecuteUnpark(&stdout); err != nil {
		t.Fatalf("ExecuteUnpark: %v", err)
	}
	var output unparkOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	if output.Integration != "parent-metadata-only" || output.ParkID != parked.ParkID {
		t.Fatalf("unpark output = %#v", output)
	}
	if fixture.st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("task status = %s", fixture.st.TaskStatus())
	}
}

func TestParkRejectsNonWaitingStatuses(t *testing.T) {
	fixture := newParkFixture(t)
	if err := fixture.st.SetTaskStatus(state.TaskStatusActive); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	err := fixture.w.ExecutePark(&stdout)
	var workerErr *WorkerError
	if !errors.As(err, &workerErr) {
		t.Fatalf("park error = %T, want WorkerError: %v", err, err)
	}
	if !strings.Contains(workerErr.Message, "親判断待ち") {
		t.Fatalf("park error = %q", workerErr.Message)
	}
}

func TestUnparkWithoutParkedTaskIsRejected(t *testing.T) {
	fixture := newParkFixture(t)
	var stdout bytes.Buffer
	err := fixture.w.ExecuteUnpark(&stdout)
	var workerErr *WorkerError
	if !errors.As(err, &workerErr) {
		t.Fatalf("unpark error = %T, want WorkerError: %v", err, err)
	}
}

func TestUnparkCleansParkCycleArtifacts(t *testing.T) {
	fixture := newParkFixture(t)
	parked := fixture.park(t)

	if _, err := os.Stat(fixture.st.ParkContentPath("parked-dirty.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.st.AttachSiblingStore(config.RepoHashFor(fixture.worktree)).LoadParkOrigin(); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	if err := fixture.w.ExecuteUnpark(&stdout); err != nil {
		t.Fatalf("ExecuteUnpark: %v", err)
	}

	if fixture.st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("task status = %s, want restored", fixture.st.TaskStatus())
	}
	if _, err := os.Stat(fixture.worktree); !os.IsNotExist(err) {
		t.Fatalf("interrupt worktree still exists: %v", err)
	}
	branchList := exec.Command("git", "-C", fixture.repo, "branch", "--list", parked.Branch)
	if output, err := branchList.Output(); err != nil || strings.TrimSpace(string(output)) != "" {
		t.Fatalf("park branch %s still exists: %v %q", parked.Branch, err, output)
	}
	if _, err := fixture.st.LoadParkRecord(); !errors.Is(err, state.ErrNoParkRecord) {
		t.Fatalf("park record still exists: %v", err)
	}
	if _, err := os.Stat(fixture.st.ParkContentPath("parked-dirty.md")); !os.IsNotExist(err) {
		t.Fatalf("park content copy still exists: %v", err)
	}
	if _, err := fixture.st.AttachSiblingStore(config.RepoHashFor(fixture.worktree)).LoadParkOrigin(); !errors.Is(err, state.ErrNoParkRecord) {
		t.Fatalf("park origin record still exists: %v", err)
	}
	if content, err := os.ReadFile(filepath.Join(fixture.repo, "parked-dirty.md")); err != nil || string(content) != "parked task working set\n" {
		t.Fatalf("parked working set changed: %q err = %v", string(content), err)
	}
}
