package app

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestParseCommandParentHandoff(t *testing.T) {
	command, err := ParseCommand([]string{"--handoff"})
	if err != nil {
		t.Fatal(err)
	}
	if command.Mode != ModeHandoff || command.Payload != "" {
		t.Fatalf("handoff command = %#v", command)
	}
	recovery, err := ParseCommand([]string{"--handoff", "recovery"})
	if err != nil {
		t.Fatal(err)
	}
	if recovery.Mode != ModeHandoff || recovery.Payload != "recovery" {
		t.Fatalf("recovery handoff command = %#v", recovery)
	}
	if _, err := ParseCommand([]string{"--handoff", "extra"}); err == nil {
		t.Fatal("--handoff accepted an unknown projection")
	}
	if _, err := ParseCommand([]string{"--handoff", "recovery", "extra"}); err == nil {
		t.Fatal("--handoff recovery accepted an extra argument")
	}
}

func TestParentHandoffPassRequiresAcceptThenBecomesNoAction(t *testing.T) {
	cfg := newAppConfig(t)
	st := startParentHandoffTask(t, cfg)
	if err := state.CaptureGitBaseline(cfg, st); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusComplete); err != nil {
		t.Fatal(err)
	}
	st.RecordSolResult(packet.Result{Status: packet.StatusPass, Risk: packet.RiskLow}, state.ParentReviewProducer{})
	taskID, err := st.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	st.RecordModelCallLog(state.ModelCallLog{
		CallID:       "call-pass",
		CallType:     state.CallTypeTask,
		TaskID:       taskID,
		Phase:        "reviewer-1",
		Role:         state.ReviewerRole,
		ModelAlias:   "haiku",
		Outcome:      "success",
		PacketStatus: string(packet.StatusPass),
	})

	var output parentHandoffOutput
	executeCommandOutput(t, cfg, ModeHandoff, &output, "--handoff")
	if !output.Consistent || output.RequiredAction == nil || *output.RequiredAction != string(state.ParentActionAccept) {
		t.Fatalf("PASS handoff = %#v", output)
	}
	if len(output.AllowedActions) != 1 || output.AllowedActions[0] != string(state.ParentActionAccept) {
		t.Fatalf("PASS allowed actions = %#v", output.AllowedActions)
	}
	if output.ParentReviewOpen == nil || *output.ParentReviewOpen != string(packet.StatusPass) {
		t.Fatalf("PASS parent review = %#v", output.ParentReviewOpen)
	}
	if output.Snapshot == nil || output.Baseline == nil || output.Baseline.Status == "" || output.Baseline.WorktreePatch == "" || output.Baseline.IndexPatch == "" {
		t.Fatalf("handoff evidence = %#v", output)
	}
	if output.LastMaterial == nil || output.LastMaterial.CallID == nil || *output.LastMaterial.CallID != "call-pass" {
		t.Fatalf("last material = %#v", output.LastMaterial)
	}

	accepted, err := st.AcceptParentReview()
	if err != nil || !accepted {
		t.Fatalf("accept = %v err=%v", accepted, err)
	}
	output = buildParentHandoff(st)
	if !output.Consistent || output.RequiredAction == nil || *output.RequiredAction != string(state.ParentActionNone) || output.ParentReviewOpen != nil {
		t.Fatalf("accepted handoff = %#v", output)
	}
}

func TestParentHandoffRecoveryProjectionOmitsBroadEvidence(t *testing.T) {
	cfg := newAppConfig(t)
	st := startParentHandoffTask(t, cfg)
	if err := state.CaptureGitBaseline(cfg, st); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusComplete); err != nil {
		t.Fatal(err)
	}
	st.RecordSolResult(packet.Result{Status: packet.StatusPass, Risk: packet.RiskLow}, state.ParentReviewProducer{})
	taskID, err := st.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	st.RecordModelCallLog(state.ModelCallLog{
		CallID:            "call-recovery",
		CallType:          state.CallTypeTask,
		TaskID:            taskID,
		Phase:             "reviewer-1",
		Role:              state.ReviewerRole,
		ModelAlias:        "haiku",
		Outcome:           "invalid_packet",
		PacketStatus:      string(packet.StatusPass),
		PacketRejectReason: "structured-output",
		Error:             "packet validation failed",
	})

	var stdout bytes.Buffer
	if err := Execute(Command{Mode: ModeHandoff, Payload: "recovery"}, cfg, nil, &stdout, io.Discard); err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &raw); err != nil {
		t.Fatalf("recovery output is not JSON: %v\n%s", err, stdout.String())
	}
	for _, forbidden := range []string{"baseline", "snapshot", "artifact_dir", "validations", "resume_kind", "inconsistency"} {
		if _, exists := raw[forbidden]; exists {
			t.Fatalf("recovery output leaked %q: %s", forbidden, stdout.String())
		}
	}
	if raw["projection"] != "recovery" || raw["consistent"] != true {
		t.Fatalf("recovery identity = %s", stdout.String())
	}
	material, ok := raw["last_material"].(map[string]any)
	if !ok || material["call_id"] != "call-recovery" || material["outcome"] != "invalid_packet" {
		t.Fatalf("recovery material = %#v", raw["last_material"])
	}
	if material["packet_reject_reason"] != "structured-output" || material["packet_error"] != "packet validation failed" {
		t.Fatalf("recovery diagnostics = %#v", material)
	}
	for _, forbidden := range []string{"role", "model"} {
		if _, exists := material[forbidden]; exists {
			t.Fatalf("recovery material leaked %q: %s", forbidden, stdout.String())
		}
	}

	stdout.Reset()
	if err := Execute(Command{Mode: ModeHandoff}, cfg, nil, &stdout, io.Discard); err != nil {
		t.Fatal(err)
	}
	raw = nil
	if err := json.Unmarshal(stdout.Bytes(), &raw); err != nil {
		t.Fatalf("full handoff output is not JSON: %v\n%s", err, stdout.String())
	}
	material, ok = raw["last_material"].(map[string]any)
	if !ok {
		t.Fatalf("full handoff material = %#v", raw["last_material"])
	}
	for _, hidden := range []string{"packet_reject_reason", "packet_error"} {
		if _, exists := material[hidden]; exists {
			t.Fatalf("full handoff exposed recovery-only %q: %s", hidden, stdout.String())
		}
	}
}

func TestParentHandoffFailsClosedOnLifecycleContradiction(t *testing.T) {
	cfg := newAppConfig(t)
	st := startParentHandoffTask(t, cfg)
	if err := st.SetTaskStatus(state.TaskStatusWaitingDecision); err != nil {
		t.Fatal(err)
	}

	output := buildParentHandoff(st)
	if output.Consistent || output.Inconsistency == nil || output.RequiredAction != nil || len(output.AllowedActions) != 0 {
		t.Fatalf("contradictory handoff = %#v", output)
	}
	if !strings.Contains(*output.Inconsistency, "lifecycle inconsistency") {
		t.Fatalf("inconsistency = %q", *output.Inconsistency)
	}
}

func TestParentHandoffValidationReferencesMatchCurrentSnapshot(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := state.CaptureGitSnapshot(cfg.RepoRoot)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	matching := qualityGateRunRecord{
		ValidationRunID: strings.Repeat("a", 32),
		Form:            "go-test",
		Repository:      cfg.RepoRoot,
		WorkingDir:      cfg.RepoRoot,
		Head:            snapshot.Head,
		IndexDigest:     snapshot.IndexDigest,
		WorktreeDigest:  snapshot.WorktreeDigest,
		StartedAt:       now,
		Status:          qualityGateStatusPass,
		Log:             "/evidence/current/gate.log",
	}
	stale := matching
	stale.ValidationRunID = strings.Repeat("b", 32)
	stale.StartedAt = now.Add(time.Minute)
	stale.WorktreeDigest = "stale-worktree"
	stale.WorkingDir = "/evidence/stale/working-dir"
	stale.Log = "/evidence/stale/gate.log"
	race := matching
	race.ValidationRunID = strings.Repeat("c", 32)
	race.Form = "go-test-race"
	race.StartedAt = now.Add(2 * time.Minute)
	race.Status = qualityGateStatusRunning
	race.Log = "/evidence/race/gate.log"
	for _, record := range []qualityGateRunRecord{matching, stale, race} {
		if err := writeQualityGateRun(st, record); err != nil {
			t.Fatal(err)
		}
	}

	output := buildParentHandoff(st)
	if !output.Consistent || len(output.Validations) != 2 {
		t.Fatalf("validations = %#v", output.Validations)
	}
	if output.Validations[0].Form != "go-test" || output.Validations[0].ValidationRunID != matching.ValidationRunID || output.Validations[0].WorkingDir != matching.WorkingDir || output.Validations[0].Log != matching.Log {
		t.Fatalf("go-test validation = %#v", output.Validations[0])
	}
	if output.Validations[1].Form != "go-test-race" || output.Validations[1].ValidationRunID != race.ValidationRunID || output.Validations[1].WorkingDir != race.WorkingDir {
		t.Fatalf("race validation = %#v", output.Validations[1])
	}
	for _, validation := range output.Validations {
		if validation.ValidationRunID == stale.ValidationRunID || validation.WorkingDir == stale.WorkingDir {
			t.Fatalf("stale snapshot validation leaked into handoff: %#v", validation)
		}
	}
}

func startParentHandoffTask(t *testing.T, cfg config.AppConfig) *state.StateStore {
	t.Helper()
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	return st
}
