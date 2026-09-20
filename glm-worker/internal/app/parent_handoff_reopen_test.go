package app

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func newAwaitingParentCompletionHandoff(t *testing.T) (config.AppConfig, *state.StateStore) {
	t.Helper()
	cfg := newAppConfig(t)
	st := startParentHandoffTask(t, cfg)
	if err := st.SaveCurrentTaskAuthority("IMPLEMENTATION_TASKS/active.md", []byte("# active\n")); err != nil {
		t.Fatal(err)
	}
	if err := state.CaptureGitBaseline(cfg, st); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusComplete); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordSolResult(packet.Result{Status: packet.StatusPass, Risk: packet.RiskHigh}, state.ParentReviewProducer{}); err != nil {
		t.Fatal(err)
	}
	accepted, err := st.AcceptParentReview()
	if err != nil || !accepted {
		t.Fatalf("accept = %v err=%v", accepted, err)
	}
	return cfg, st
}

func saveHandoffPublicationCandidate(t *testing.T, st *state.StateStore) state.PublicationCandidate {
	t.Helper()
	taskID, err := st.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	head := strings.Repeat("1", 40)
	snapshot := state.SnapshotDigest{Head: head, IndexDigest: strings.Repeat("a", 64), WorktreeDigest: strings.Repeat("b", 64)}
	candidate := state.PublicationCandidate{
		Version:       1,
		TaskID:        taskID,
		BaseHead:      head,
		CommitOID:     head,
		TreeOID:       strings.Repeat("c", 40),
		MessageDigest: strings.Repeat("d", 64),
		Snapshot:      snapshot,
		SnapshotID:    state.ValidationSnapshotID(snapshot.Head, snapshot.IndexDigest, snapshot.WorktreeDigest),
		PreparedAt:    time.Now().UTC(),
	}
	if err := st.SavePublicationCandidate(candidate); err != nil {
		t.Fatal(err)
	}
	return candidate
}

func handoffAllowsAction(actions []string, action string) bool {
	for _, value := range actions {
		if value == action {
			return true
		}
	}
	return false
}

func TestParentHandoffAwaitingParentCompletionProjectsMachineDecision(t *testing.T) {
	_, st := newAwaitingParentCompletionHandoff(t)
	saveHandoffPublicationCandidate(t, st)

	before := buildParentHandoff(st)
	if !before.Consistent {
		t.Fatalf("awaiting handoff inconsistent: %#v", before)
	}
	if before.TaskStatus == nil || *before.TaskStatus != string(state.TaskStatusAwaitingParentCompletion) {
		t.Fatalf("awaiting handoff task status = %#v", before.TaskStatus)
	}
	if before.RequiredAction == nil || *before.RequiredAction != string(state.ParentActionComplete) {
		t.Fatalf("awaiting required action before finding = %#v", before.RequiredAction)
	}
	if !handoffAllowsAction(before.AllowedActions, string(state.ParentActionComplete)) || handoffAllowsAction(before.AllowedActions, string(state.ParentActionReopen)) {
		t.Fatalf("awaiting allowed actions before finding = %#v", before.AllowedActions)
	}
	assertHandoffJSONOmitsReopenSpec(t, before)

	if _, err := st.RecordPublicationInvalidatingFinding(
		state.ParentOriginCodexReview,
		state.ParentCauseProductionWiring,
	); err != nil {
		t.Fatal(err)
	}
	after := buildParentHandoff(st)
	if !after.Consistent {
		t.Fatalf("finding-backed handoff inconsistent: %#v", after)
	}
	if after.RequiredAction == nil || *after.RequiredAction != string(state.ParentActionReopen) {
		t.Fatalf("finding-backed required action = %#v", after.RequiredAction)
	}
	if !handoffAllowsAction(after.AllowedActions, string(state.ParentActionReopen)) || handoffAllowsAction(after.AllowedActions, string(state.ParentActionComplete)) || handoffAllowsAction(after.AllowedActions, string(state.ParentActionInstall)) {
		t.Fatalf("finding-backed allowed actions = %#v", after.AllowedActions)
	}
	assertHandoffJSONExposesReopenSpec(t, after)

	recovery := projectParentHandoffRecovery(after)
	if recovery.TaskStatus == nil || *recovery.TaskStatus != string(state.TaskStatusAwaitingParentCompletion) {
		t.Fatalf("awaiting recovery handoff task status = %#v", recovery.TaskStatus)
	}
	if recovery.RequiredAction == nil || *recovery.RequiredAction != string(state.ParentActionReopen) {
		t.Fatalf("recovery required action = %#v", recovery.RequiredAction)
	}
	assertHandoffJSONExposesReopenSpec(t, recovery)
}

func TestParentHandoffAfterReopenProjectsWaitingSolReviewWithFixAction(t *testing.T) {
	_, st := newAwaitingParentCompletionHandoff(t)
	saveHandoffPublicationCandidate(t, st)
	if _, err := st.RecordPublicationInvalidatingFinding(
		state.ParentOriginCodexReview,
		state.ParentCauseProductionWiring,
	); err != nil {
		t.Fatal(err)
	}
	if err := st.ReopenAcceptedParentCompletion(); err != nil {
		t.Fatal(err)
	}

	output := buildParentHandoff(st)
	if !output.Consistent {
		t.Fatalf("reopened handoff inconsistent: %#v", output)
	}
	if output.TaskStatus == nil || *output.TaskStatus != string(state.TaskStatusWaitingSolReview) {
		t.Fatalf("reopened handoff task status = %#v", output.TaskStatus)
	}
	if output.RequiredAction == nil || *output.RequiredAction != string(state.ParentActionReview) {
		t.Fatalf("reopened required action = %#v", output.RequiredAction)
	}
	if !handoffAllowsAction(output.AllowedActions, string(state.ParentActionFix)) {
		t.Fatalf("reopened allowed actions lack fix: %#v", output.AllowedActions)
	}
	if handoffAllowsAction(output.AllowedActions, string(state.ParentActionAccept)) || handoffAllowsAction(output.AllowedActions, string(state.ParentActionComplete)) || handoffAllowsAction(output.AllowedActions, string(state.ParentActionReopen)) {
		t.Fatalf("reopened allowed actions keep terminal actions: %#v", output.AllowedActions)
	}
	if output.Publication != nil {
		t.Fatalf("reopened handoff keeps publication projection: %#v", output.Publication)
	}
	recovery := projectParentHandoffRecovery(output)
	if recovery.TaskStatus == nil || *recovery.TaskStatus != string(state.TaskStatusWaitingSolReview) {
		t.Fatalf("reopened recovery handoff task status = %#v", recovery.TaskStatus)
	}
	if !handoffAllowsAction(recovery.AllowedActions, string(state.ParentActionFix)) {
		t.Fatalf("reopened recovery allowed actions = %#v", recovery.AllowedActions)
	}
}

func assertHandoffJSONExposesReopenSpec(t *testing.T, value any) {
	t.Helper()
	data, specs := decodeHandoffActionSpecs(t, value)
	spec, ok := specs[string(state.ParentActionReopen)]
	if !ok || spec.Kind != "direct" {
		t.Fatalf("handoff JSON lacks a direct reopen action spec: %s", data)
	}
	want := []string{"glm-parent-action", "reopen"}
	if !reflect.DeepEqual(spec.Command, want) {
		t.Fatalf("reopen action spec command = %#v want %#v", spec.Command, want)
	}
}

func assertHandoffJSONOmitsReopenSpec(t *testing.T, value any) {
	t.Helper()
	data, specs := decodeHandoffActionSpecs(t, value)
	if _, ok := specs[string(state.ParentActionReopen)]; ok {
		t.Fatalf("handoff JSON exposes reopen without invalidating finding: %s", data)
	}
}

func decodeHandoffActionSpecs(t *testing.T, value any) ([]byte, map[string]struct {
	Kind    string   `json:"kind"`
	Command []string `json:"command"`
}) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		ActionSpecs map[string]struct {
			Kind    string   `json:"kind"`
			Command []string `json:"command"`
		} `json:"action_specs"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	return data, decoded.ActionSpecs
}
