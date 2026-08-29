package app

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestQualityGateRecoveryCommandParsesRunSurfaces(t *testing.T) {
	runID := strings.Repeat("a", 32)
	for _, action := range []string{"status", "watch", "result"} {
		cmd, err := ParseCommand([]string{"--quality-gate", action, runID})
		if err != nil {
			t.Fatalf("%s parse: %v", action, err)
		}
		if cmd.Mode != ModeQualityGate || cmd.Payload != action+":"+runID {
			t.Fatalf("%s command = %+v", action, cmd)
		}
	}
	if _, err := ParseCommand([]string{"--quality-gate", "status", "../../state"}); err == nil {
		t.Fatal("path-like validation run id must fail closed")
	}
}

func TestQualityGateRunningIdentityMatchesOnlyExactSnapshot(t *testing.T) {
	_, st := newQualityGateEnv(t)
	runID := strings.Repeat("b", 32)
	snapshot := state.GitSnapshot{Head: "head-a", IndexDigest: "index-a", WorktreeDigest: "worktree-a"}
	record := qualityGateRunRecord{
		ValidationRunID: runID,
		Form:            "go-test",
		Repository:      "/repo",
		WorkingDir:      "/repo/glm-worker",
		Head:            snapshot.Head,
		IndexDigest:     snapshot.IndexDigest,
		WorktreeDigest:  snapshot.WorktreeDigest,
		StartedAt:       time.Now().UTC(),
		Status:          "running",
	}
	if err := writeQualityGateRun(st, record); err != nil {
		t.Fatal(err)
	}
	got, found := findRunningQualityGateRun(st, "go-test", "/repo", snapshot)
	if !found || got.ValidationRunID != runID {
		t.Fatalf("same snapshot running gate not found: found=%v record=%+v", found, got)
	}
	changed := snapshot
	changed.WorktreeDigest = "worktree-b"
	if _, found := findRunningQualityGateRun(st, "go-test", "/repo", changed); found {
		t.Fatal("changed snapshot reused a running gate")
	}
	if _, found := findRunningQualityGateRun(st, "go-test-race", "/repo", snapshot); found {
		t.Fatal("different form reused a running gate")
	}
}

func TestQualityGateCompletedRunIsNotReused(t *testing.T) {
	_, st := newQualityGateEnv(t)
	runID := strings.Repeat("c", 32)
	snapshot := state.GitSnapshot{Head: "head", IndexDigest: "index", WorktreeDigest: "worktree"}
	record := qualityGateRunRecord{
		ValidationRunID: runID,
		Form:            "go-test",
		Repository:      "/repo",
		Head:            snapshot.Head,
		IndexDigest:     snapshot.IndexDigest,
		WorktreeDigest:  snapshot.WorktreeDigest,
		StartedAt:       time.Now().Add(-time.Second).UTC(),
		Status:          "pass",
	}
	if err := writeQualityGateRun(st, record); err != nil {
		t.Fatal(err)
	}
	if _, found := findRunningQualityGateRun(st, "go-test", "/repo", snapshot); found {
		t.Fatal("completed validation must not be reused as a running attachment")
	}
}

func TestQualityGateRunLogsRemainAddressablePerRun(t *testing.T) {
	_, st := newQualityGateEnv(t)
	firstID := strings.Repeat("d", 32)
	secondID := strings.Repeat("e", 32)
	firstPath, err := writeQualityGateRunLog(st, firstID, []byte("first\n"))
	if err != nil {
		t.Fatal(err)
	}
	secondPath, err := writeQualityGateRunLog(st, secondID, []byte("second\n"))
	if err != nil {
		t.Fatal(err)
	}
	if firstPath == secondPath {
		t.Fatalf("run logs share path: %q", firstPath)
	}
	first, _ := os.ReadFile(firstPath)
	second, _ := os.ReadFile(secondPath)
	if string(first) != "first\n" || string(second) != "second\n" {
		t.Fatalf("run log evidence overwritten: first=%q second=%q", first, second)
	}
}

func TestQualityGateStatusIsMachineReadableByRunID(t *testing.T) {
	_, st := newQualityGateEnv(t)
	runID := strings.Repeat("f", 32)
	record := qualityGateRunRecord{
		ValidationRunID: runID,
		Form:            "go-test",
		Repository:      "/repo",
		Head:            "head",
		IndexDigest:     "index",
		WorktreeDigest:  "worktree",
		StartedAt:       time.Now().UTC(),
		Status:          "running",
	}
	if err := writeQualityGateRun(st, record); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	if err := printQualityGateRun(st, runID, false, &stdout); err != nil {
		t.Fatal(err)
	}
	var got qualityGateRunRecord
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("status is not JSON: %v: %s", err, stdout.String())
	}
	if got.ValidationRunID != runID || got.Status != "running" {
		t.Fatalf("status output = %+v", got)
	}
}
