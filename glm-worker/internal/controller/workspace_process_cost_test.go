package controller

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestWorkspaceProcessCostAcrossMaterializationAndSourceImport(t *testing.T) {
	source, _ := newControllerLinkedWorktree(t)
	for index := range 257 {
		path := filepath.Join(source, fmt.Sprintf("cost-%03d.txt", index))
		if err := os.WriteFile(path, []byte(fmt.Sprintf("content-%d\n", index)), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	runControllerGit(t, source, "add", "--all")
	runControllerGit(t, source, "commit", "-qm", "workspace cost fixture")
	tree := controllerGitOutput(t, source, "rev-parse", "HEAD^{tree}")
	entries, err := sourceImportTreeEntries(source, tree)
	if err != nil {
		t.Fatal(err)
	}
	lane := filepath.Join(t.TempDir(), "lane")
	runControllerGit(t, source, "worktree", "add", "--detach", "--no-checkout", lane, "HEAD")
	operations := []struct {
		name string
		run  func() error
	}{
		{name: "materialize", run: func() error {
			return materializeExecutionTrees(WorkspaceIdentity{Root: lane}, ReboundSuspension{Current: ExecutionTrees{IndexTree: tree, WorktreeTree: tree}})
		}},
		{name: "source-import", run: func() error {
			return applySourceTreeEntries(lane, entries, entries)
		}},
	}
	for _, operation := range operations {
		t.Run(operation.name, func(t *testing.T) {
			trace := filepath.Join(t.TempDir(), "git-trace.jsonl")
			t.Setenv("GIT_TRACE2_EVENT", trace)
			if err := operation.run(); err != nil {
				t.Fatal(err)
			}
			starts := workspaceGitProcessCount(t, trace)
			if starts == 0 || starts > len(entries)/8 {
				t.Fatalf("workspace processing launched too many Git processes: files=%d processes=%d", len(entries), starts)
			}
			for _, entry := range entries {
				got, err := os.ReadFile(filepath.Join(lane, entry.path))
				want, sourceErr := os.ReadFile(filepath.Join(source, entry.path))
				if err != nil || sourceErr != nil || !bytes.Equal(got, want) {
					t.Fatalf("workspace contents changed at %q: %v %v", entry.path, err, sourceErr)
				}
			}
			t.Logf("files=%d Git processes=%d", len(entries), starts)
		})
	}
}

func workspaceGitProcessCount(t *testing.T, trace string) int {
	t.Helper()
	data, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	starts := 0
	for _, line := range bytes.Split(data, []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		var event struct {
			Event string `json:"event"`
		}
		if err := json.Unmarshal(line, &event); err != nil {
			t.Fatal(err)
		}
		if event.Event == "start" {
			starts++
		}
	}
	return starts
}
