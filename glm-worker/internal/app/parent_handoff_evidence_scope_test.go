package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentcontinuation"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestParentHandoffEvidenceReadScopeFullVersusRecovery(t *testing.T) {
	t.Parallel()
	cfg := newAppConfig(t)
	st := startParentHandoffTask(t, cfg)
	if err := state.CaptureGitBaseline(cfg, st); err != nil {
		t.Fatal(err)
	}
	snapshot, err := state.CaptureGitSnapshot(cfg.RepoRoot)
	if err != nil {
		t.Fatal(err)
	}
	const runs = 24
	for i := 0; i < runs; i++ {
		record := qualitygate.RunRecord{}
		record.ValidationRunID = fmt.Sprintf("%032x", i+1)
		record.Form = "go-test"
		record.Repository = cfg.RepoRoot
		record.WorkingDir = cfg.RepoRoot
		record.Head = snapshot.Head
		record.IndexDigest = snapshot.IndexDigest
		record.WorktreeDigest = snapshot.WorktreeDigest
		record.StartedAt = time.Now().UTC().Add(time.Duration(i) * time.Second)
		record.Status = qualitygate.StatusPass
		if err := writeQualityGateRun(st, record); err != nil {
			t.Fatal(err)
		}
	}
	continuation := parentcontinuation.BuildWithRepositoryPolicy(cfg, st)
	fullVisits := &parentHandoffEvidenceReadVisits{}
	full := buildParentHandoffFromContinuationWithEvidence(st, continuation, true, fullVisits)
	narrowVisits := &parentHandoffEvidenceReadVisits{}
	narrow := buildParentHandoffFromContinuationWithEvidence(st, continuation, false, narrowVisits)

	if full.Baseline == nil || full.Snapshot == nil || len(full.Validations) != 1 {
		t.Fatalf("full evidence missing: baseline=%t snapshot=%t validations=%d", full.Baseline != nil, full.Snapshot != nil, len(full.Validations))
	}
	if full.Validations[0].ValidationRunID != fmt.Sprintf("%032x", runs) {
		t.Fatalf("full handoff lost newest current proof: %#v", full.Validations)
	}
	if fullVisits.RunRecords != runs || fullVisits.RunEntries < runs {
		t.Fatalf("wide source reads: %+v, want %d run records", fullVisits, runs)
	}
	if narrowVisits.RunRecords != 0 || narrowVisits.RunEntries != 0 {
		t.Fatalf("recovery enumerated unneeded quality history: %+v", narrowVisits)
	}
	if narrow.Baseline != nil || narrow.Snapshot != nil || len(narrow.Validations) != 0 {
		t.Fatalf("narrow view retained broad evidence: %#v", narrow)
	}
	if !reflect.DeepEqual(projectParentHandoffRecovery(full), projectParentHandoffRecovery(narrow)) {
		t.Fatal("recovery semantics changed when optional full evidence was omitted")
	}
	wideJSON, err := json.Marshal(full)
	if err != nil {
		t.Fatal(err)
	}
	narrowJSON, err := json.Marshal(narrow)
	if err != nil {
		t.Fatal(err)
	}
	if len(narrowJSON) >= len(wideJSON) {
		t.Fatalf("intermediate handoff JSON not reduced: wide=%d narrow=%d", len(wideJSON), len(narrowJSON))
	}
	t.Logf("run records: full=%d recovery=%d", fullVisits.RunRecords, narrowVisits.RunRecords)
	t.Logf("run entries: full=%d recovery=%d", fullVisits.RunEntries, narrowVisits.RunEntries)
	t.Logf("intermediate JSON bytes: full=%d bounded=%d", len(wideJSON), len(narrowJSON))
	assertTerminalHandoffOmitsOnlyOptionalEvidence(t, cfg, st)

	var recovery bytes.Buffer
	if err := printParentHandoffRecoveryLeasedWithConfig(cfg, st, &recovery); err != nil {
		t.Fatal(err)
	}
	var actual map[string]json.RawMessage
	if err := json.Unmarshal(recovery.Bytes(), &actual); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"baseline", "snapshot", "validations"} {
		if _, found := actual[key]; found {
			t.Fatalf("recovery leaked %s", key)
		}
	}
	if !bytes.Equal(actual["projection"], []byte(`"recovery"`)) {
		t.Fatalf("recovery projection missing: %s", recovery.String())
	}
}

func assertTerminalHandoffOmitsOnlyOptionalEvidence(t *testing.T, cfg config.AppConfig, st *state.StateStore) {
	t.Helper()
	readView := func(args ...string) (map[string]json.RawMessage, int) {
		t.Helper()
		command, err := parentHandoffCommand(append([]string{"--handoff"}, args...))
		if err != nil {
			t.Fatal(err)
		}
		var output bytes.Buffer
		if err := executeHandoffInspection(command, cfg, st, &output); err != nil {
			t.Fatal(err)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(output.Bytes(), &fields); err != nil {
			t.Fatal(err)
		}
		return fields, output.Len()
	}
	full, fullBytes := readView()
	terminal, terminalBytes := readView(handoffTerminalView)
	for key, empty := range map[string]string{"baseline": "null", "snapshot": "null", "validations": "[]"} {
		if len(full[key]) == 0 || bytes.Equal(full[key], []byte(empty)) || !bytes.Equal(terminal[key], []byte(empty)) {
			t.Fatalf("%s did not differ as expected: full=%s terminal=%s", key, full[key], terminal[key])
		}
		delete(full, key)
		delete(terminal, key)
	}
	if !reflect.DeepEqual(full, terminal) {
		t.Fatal("terminal view changed handoff authority or actions")
	}
	if terminalBytes >= fullBytes {
		t.Fatalf("terminal handoff bytes=%d, full bytes=%d", terminalBytes, fullBytes)
	}
	t.Logf("handoff CLI JSON bytes: full=%d terminal=%d", fullBytes, terminalBytes)
}
