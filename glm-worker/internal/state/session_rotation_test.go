package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type boundRotationAfterLaterTaskSeed struct {
	st        *StateStore
	oldThread string
	newThread string
	claim     SessionRotationClaim
}

func TestDecideSessionRotationRuleTable(t *testing.T) {
	base := func() SessionRotationSignals {
		return SessionRotationSignals{
			Terminal:                SessionRotationTerminalAccept,
			AcceptedTasks:           1,
			CurrentAcceptedRisk:     "LOW",
			Rollout:                 &SessionRotationRolloutSignals{ModelTurns: 1, ToolOutputBytes: 1, Compactions: 0, Source: "rollout.jsonl"},
			MaterialEvents:          1,
			MaterialEventsAvailable: true,
			Limit:                   &SessionRotationLimitSignals{UsedDeltaPoints: 0, LimitID: "codex"},
			LimitSource:             "rotation/marker.json",
		}
	}
	tests := []struct {
		name   string
		mutate func(*SessionRotationSignals)
		want   bool
		reason string
	}{
		{"first accepted task stays on session", func(s *SessionRotationSignals) { s.AcceptedTasks = 1 }, false, ""},
		{"second accepted task rotates by default", func(s *SessionRotationSignals) { s.AcceptedTasks = 2 }, true, SessionRotationReasonDefaultTwoTasks},
		{"high risk accept rotates on first task", func(s *SessionRotationSignals) { s.CurrentAcceptedRisk = "HIGH" }, true, SessionRotationReasonHighRisk},
		{"high risk is not an accept terminal on no-go", func(s *SessionRotationSignals) {
			s.Terminal = SessionRotationTerminalNoGo
			s.CurrentAcceptedRisk = "HIGH"
		}, false, ""},
		{"no-go does not count accepted tasks", func(s *SessionRotationSignals) {
			s.Terminal = SessionRotationTerminalNoGo
			s.AcceptedTasks = 2
		}, false, ""},
		{"single compaction rotates", func(s *SessionRotationSignals) { s.Rollout.Compactions = 1 }, true, SessionRotationReasonCompaction},
		{"five model turns stay on session", func(s *SessionRotationSignals) { s.Rollout.ModelTurns = 5 }, false, ""},
		{"six model turns rotate", func(s *SessionRotationSignals) { s.Rollout.ModelTurns = 6 }, true, SessionRotationReasonModelTurns},
		{"tool output below threshold stays", func(s *SessionRotationSignals) { s.Rollout.ToolOutputBytes = 262143 }, false, ""},
		{"tool output at threshold rotates", func(s *SessionRotationSignals) { s.Rollout.ToolOutputBytes = 262144 }, true, SessionRotationReasonToolOutputBytes},
		{"single material event stays", func(s *SessionRotationSignals) { s.MaterialEvents = 1 }, false, ""},
		{"two material events rotate", func(s *SessionRotationSignals) { s.MaterialEvents = 2 }, true, SessionRotationReasonRepeatedEvents},
		{"limit delta below ten points stays", func(s *SessionRotationSignals) { s.Limit.UsedDeltaPoints = 9.999 }, false, ""},
		{"limit delta at ten points rotates", func(s *SessionRotationSignals) { s.Limit.UsedDeltaPoints = 10 }, true, SessionRotationReasonLimitWindow},
		{"limit window reset does not rotate on delta", func(s *SessionRotationSignals) {
			s.Limit.WindowReset = true
			s.Limit.UsedDeltaPoints = 80
		}, false, ""},
		{"rollout evidence missing rotates conservatively", func(s *SessionRotationSignals) {
			s.Rollout = nil
			s.RolloutUnavailableField = SessionRotationEvidenceFieldRolloutAssociation
			s.RolloutUnavailableSrc = "no rollout"
		}, true, SessionRotationReasonEvidenceUnavailable},
		{"material event evidence missing rotates conservatively", func(s *SessionRotationSignals) {
			s.MaterialEventsAvailable = false
			s.MaterialEvents = 0
		}, true, SessionRotationReasonEvidenceUnavailable},
		{"live limit evidence missing rotates conservatively", func(s *SessionRotationSignals) {
			s.Limit = nil
			s.LimitUnavailableFields = []string{SessionRotationEvidenceFieldLimitLive}
		}, true, SessionRotationReasonEvidenceUnavailable},
		{"saved limit baseline missing rotates conservatively", func(s *SessionRotationSignals) {
			s.Limit = nil
			s.LimitUnavailableFields = []string{SessionRotationEvidenceFieldLimitBaseline}
		}, true, SessionRotationReasonEvidenceUnavailable},
		{"limit id mismatch rotates conservatively", func(s *SessionRotationSignals) {
			s.Limit = nil
			s.LimitUnavailableFields = []string{SessionRotationEvidenceFieldLimitWindow}
		}, true, SessionRotationReasonEvidenceUnavailable},
		{"real trigger wins over missing evidence", func(s *SessionRotationSignals) {
			s.Rollout = nil
			s.RolloutUnavailableField = SessionRotationEvidenceFieldRolloutAssociation
			s.AcceptedTasks = 2
		}, true, SessionRotationReasonDefaultTwoTasks},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			signals := base()
			tc.mutate(&signals)
			decision := DecideSessionRotation(signals)
			if decision.Required != tc.want || decision.Reason != tc.reason {
				t.Fatalf("decision = required:%v reason:%q want required:%v reason:%q", decision.Required, decision.Reason, tc.want, tc.reason)
			}
			if tc.want && len(decision.Evidence) == 0 {
				t.Fatalf("required decision carries no evidence: %#v", decision)
			}
		})
	}
}

func TestSessionRotationMarkerSchemaValidation(t *testing.T) {
	validDirective := func() *SessionRotationDirective {
		return &SessionRotationDirective{
			DirectiveID: "0f4a9b31-52c8-4d7e-9a31-6b2f5c8d1e40",
			TaskID:      "12345678-aaaa-bbbb-cccc-dddddddddddd",
			Terminal:    SessionRotationTerminalAccept,
			Epoch:       "12345678-aaaa-bbbb-cccc-dddddddddddd:accept",
			Reason:      SessionRotationReasonDefaultTwoTasks,
			Evidence:    []SessionRotationEvidence{{Trigger: SessionRotationReasonDefaultTwoTasks, Field: "accepted_tasks", Value: "2"}},
			CreatedAt:   "2026-09-07T00:00:00Z",
		}
	}
	base := func() SessionRotationMarker {
		return SessionRotationMarker{
			Version:        sessionRotationMarkerVersion,
			ParentThreadID: "01a0463c-d477-7410-9efd-cb34ff2e0b0e",
			State:          SessionRotationStatePending,
			Directive:      validDirective(),
			UpdatedAt:      "2026-09-07T00:00:00Z",
		}
	}
	tests := []struct {
		name   string
		mutate func(*SessionRotationMarker)
		extra  string
	}{
		{"unknown field is rejected", nil, `,"unexpected":1}`},
		{"bad version is rejected", func(m *SessionRotationMarker) { m.Version = 4 }, ""},
		{"bad thread id is rejected", func(m *SessionRotationMarker) { m.ParentThreadID = "not-a-uuid" }, ""},
		{"pending without directive is rejected", func(m *SessionRotationMarker) { m.Directive = nil }, ""},
		{"pending with issued record is rejected", func(m *SessionRotationMarker) {
			m.Issued = &SessionRotationIssued{BoundThreadID: "01a0463c-d477-7410-9efd-cb34ff2e0b0f", IssuedAt: "2026-09-07T00:00:00Z"}
		}, ""},
		{"issued without bound thread is rejected", func(m *SessionRotationMarker) {
			m.State = SessionRotationStateIssued
			m.Directive = nil
		}, ""},
		{"directive without generated uuid is rejected", func(m *SessionRotationMarker) { m.Directive.DirectiveID = "0f4a9b31-52c8-9d7e-9a31-6b2f5c8d1e40" }, ""},
		{"directive epoch mismatch is rejected", func(m *SessionRotationMarker) { m.Directive.Epoch = "other:accept" }, ""},
		{"directive unknown reason is rejected", func(m *SessionRotationMarker) { m.Directive.Reason = "vibe" }, ""},
		{"directive unknown terminal is rejected", func(m *SessionRotationMarker) {
			m.Directive.Terminal = "park"
			m.Directive.Epoch = m.Directive.TaskID + ":park"
		}, ""},
		{"baseline without limit id is rejected", func(m *SessionRotationMarker) {
			m.LimitBaseline = &SessionLimitBaseline{WindowResetsAt: 100, UsedPercent: 10, CapturedAt: "2026-09-07T00:00:00Z"}
		}, ""},
		{"baseline over hundred percent is rejected", func(m *SessionRotationMarker) {
			m.LimitBaseline = &SessionLimitBaseline{LimitID: "codex", WindowResetsAt: 100, UsedPercent: 101, CapturedAt: "2026-09-07T00:00:00Z"}
		}, ""},
		{"last evaluation without task is rejected", func(m *SessionRotationMarker) {
			m.LastEvaluation = &SessionRotationEvaluationRecord{Terminal: SessionRotationTerminalAccept, At: "2026-09-07T00:00:00Z"}
		}, ""},
		{"missing updated_at is rejected", func(m *SessionRotationMarker) { m.UpdatedAt = "" }, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			marker := base()
			if tc.mutate != nil {
				tc.mutate(&marker)
			}
			data, err := json.Marshal(marker)
			if err != nil {
				t.Fatal(err)
			}
			if tc.extra != "" {
				data = append(data[:len(data)-1], []byte(tc.extra)...)
			}
			if _, err := decodeSessionRotationMarker(data); err == nil {
				t.Fatalf("invalid markerが受理されました: %s", data)
			}
		})
	}
	if _, err := decodeSessionRotationMarker(func() []byte {
		data, _ := json.Marshal(base())
		return data
	}()); err != nil {
		t.Fatalf("valid markerが拒否されました: %v", err)
	}
}

func TestSessionRotationCommitPersistsDirectiveOncePerEpoch(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	threadID := "01a0463c-d477-7410-9efd-cb34ff2e0b0e"
	first := &SessionRotationEvaluation{
		ParentThreadID: threadID,
		TaskID:         "12345678-aaaa-bbbb-cccc-dddddddddddd",
		Terminal:       SessionRotationTerminalAccept,
		Decision:       SessionRotationDecision{Required: true, Reason: SessionRotationReasonDefaultTwoTasks, Evidence: []SessionRotationEvidence{{Trigger: SessionRotationReasonDefaultTwoTasks}}},
	}
	if err := st.commitSessionRotation(first); err != nil {
		t.Fatal(err)
	}
	marker, err := st.LoadSessionRotationMarker(threadID)
	if err != nil || marker == nil || marker.State != SessionRotationStatePending || marker.Directive == nil {
		t.Fatalf("first commit後のmarker = %#v err=%v", marker, err)
	}
	firstDirective := marker.Directive.DirectiveID

	second := &SessionRotationEvaluation{
		ParentThreadID: threadID,
		TaskID:         "87654321-aaaa-bbbb-cccc-dddddddddddd",
		Terminal:       SessionRotationTerminalAccept,
		Decision:       SessionRotationDecision{Required: true, Reason: SessionRotationReasonRepeatedEvents},
	}
	if err := st.commitSessionRotation(second); err != nil {
		t.Fatal(err)
	}
	marker, err = st.LoadSessionRotationMarker(threadID)
	if err != nil {
		t.Fatal(err)
	}
	if marker.Directive.DirectiveID != firstDirective {
		t.Fatalf("同一thread内の再評価がdirectiveを作り直しました: %s -> %s", firstDirective, marker.Directive.DirectiveID)
	}
	if marker.LastEvaluation == nil || marker.LastEvaluation.TaskID != second.TaskID || !marker.LastEvaluation.Required {
		t.Fatalf("last evaluation = %#v", marker.LastEvaluation)
	}

	notRequired := &SessionRotationEvaluation{
		ParentThreadID: threadID,
		TaskID:         "87654321-aaaa-bbbb-cccc-dddddddddddd",
		Terminal:       SessionRotationTerminalAccept,
		Decision:       SessionRotationDecision{Required: false},
	}
	if err := st.commitSessionRotation(notRequired); err != nil {
		t.Fatal(err)
	}
	marker, err = st.LoadSessionRotationMarker(threadID)
	if err != nil {
		t.Fatal(err)
	}
	if marker.State != SessionRotationStatePending || marker.Directive.DirectiveID != firstDirective {
		t.Fatalf("not-required評価がpending directiveを壊しました: %#v", marker)
	}
	if marker.LastEvaluation.Required {
		t.Fatalf("last evaluation = %#v", marker.LastEvaluation)
	}

	baseline := &SessionLimitBaseline{LimitID: "codex", WindowResetsAt: 1787685137, UsedPercent: 20, CapturedAt: "2026-09-07T00:00:00Z"}
	if err := st.commitSessionRotation(&SessionRotationEvaluation{
		ParentThreadID:      threadID,
		TaskID:              "87654321-aaaa-bbbb-cccc-dddddddddddd",
		Terminal:            SessionRotationTerminalAccept,
		Decision:            SessionRotationDecision{Required: false},
		LimitBaselineUpdate: baseline,
	}); err != nil {
		t.Fatal(err)
	}
	marker, err = st.LoadSessionRotationMarker(threadID)
	if err != nil {
		t.Fatal(err)
	}
	if marker.LimitBaseline == nil || marker.LimitBaseline.WindowResetsAt != baseline.WindowResetsAt {
		t.Fatalf("baseline更新が保存されていません: %#v", marker.LimitBaseline)
	}

	if err := st.commitSessionRotation(&SessionRotationEvaluation{
		ParentThreadID: "invalid",
		TaskID:         "12345678-aaaa-bbbb-cccc-dddddddddddd",
		Terminal:       SessionRotationTerminalAccept,
	}); err == nil {
		t.Fatal("invalid parent thread identityがfail closedしませんでした")
	}
}

func TestSessionRotationMarkerFileIsThreadKeyedAndAtomic(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	threadID := "01a0463c-d477-7410-9efd-cb34ff2e0b0e"
	other := "01a0244a-4ee4-7e71-b2e1-dec3bdda2120"
	if st.SessionRotationMarkerPath(threadID) != filepath.Join(st.dir, "rotation", threadID+".json") {
		t.Fatalf("marker path = %s", st.SessionRotationMarkerPath(threadID))
	}
	reading := SessionLimitReading{LimitID: "codex", UsedPercent: 10, ResetsAt: 1787685137, CapturedAt: time.Now().UTC()}
	if err := st.SaveSessionLimitBaseline(threadID, reading); err != nil {
		t.Fatal(err)
	}
	first, err := st.LoadSessionRotationMarker(threadID)
	if err != nil || first == nil || first.LimitBaseline == nil || first.LimitBaseline.UsedPercent != 10 {
		t.Fatalf("baseline保存後のmarker = %#v err=%v", first, err)
	}
	firstUpdatedAt := first.UpdatedAt

	if err := st.SaveSessionLimitBaseline(threadID, reading); err != nil {
		t.Fatal(err)
	}
	same, err := st.LoadSessionRotationMarker(threadID)
	if err != nil {
		t.Fatal(err)
	}
	if same.UpdatedAt != firstUpdatedAt {
		t.Fatalf("同一windowのbaseline再保存がmarkerを更新しました: %s -> %s", firstUpdatedAt, same.UpdatedAt)
	}

	reset := SessionLimitReading{LimitID: "codex", UsedPercent: 3, ResetsAt: 1787685137 + 3600, CapturedAt: time.Now().UTC()}
	if err := st.SaveSessionLimitBaseline(threadID, reset); err != nil {
		t.Fatal(err)
	}
	updated, err := st.LoadSessionRotationMarker(threadID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.LimitBaseline.WindowResetsAt != reset.ResetsAt || updated.LimitBaseline.UsedPercent != 3 {
		t.Fatalf("window reset後のbaseline = %#v", updated.LimitBaseline)
	}

	if _, err := st.LoadSessionRotationMarker(other); err != nil {
		t.Fatal(err)
	}
	if entries, err := os.ReadDir(filepath.Join(st.dir, "rotation")); err != nil || len(entries) != 1 {
		t.Fatalf("rotation dir = %#v err=%v", entries, err)
	}

	if err := os.WriteFile(filepath.Join(st.dir, "rotation", other+".json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := st.LoadSessionRotationMarker(other); err == nil {
		t.Fatal("schema不正なmarkerがfail closedしませんでした")
	}
}

func TestSessionRotationClaimBindAcknowledgeCoversOnlyClaimedDirective(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	oldThread := "01a0463c-d477-7410-9efd-cb34ff2e0b0e"
	newThread := "01a0244a-4ee4-7e71-b2e1-dec3bdda2120"
	for _, thread := range []string{oldThread, newThread} {
		if err := st.commitSessionRotation(&SessionRotationEvaluation{
			ParentThreadID: thread,
			TaskID:         "12345678-aaaa-bbbb-cccc-dddddddddddd",
			Terminal:       SessionRotationTerminalAccept,
			Decision:       SessionRotationDecision{Required: true, Reason: SessionRotationReasonCompaction},
		}); err != nil {
			t.Fatal(err)
		}
	}
	oldMarker, err := st.LoadSessionRotationMarker(oldThread)
	if err != nil {
		t.Fatal(err)
	}
	claim, err := st.ClaimSessionRotation(oldThread, oldMarker.Directive.DirectiveID)
	if err != nil {
		t.Fatal(err)
	}
	duplicate, err := st.ClaimSessionRotation(oldThread, oldMarker.Directive.DirectiveID)
	if err != nil || duplicate.ClaimID != claim.ClaimID || duplicate.TargetTaskID != claim.TargetTaskID {
		t.Fatalf("duplicate claim = %#v err=%v", duplicate, err)
	}
	if err := st.BindSessionRotationClaim(oldThread, oldMarker.Directive.DirectiveID, claim.ClaimID, newThread); err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartSessionRotationTask(newThread, claim.ClaimID); err != nil {
		t.Fatal(err)
	}
	if err := st.SetParentCodexIdentity(newThread, newThread, nil); err != nil {
		t.Fatal(err)
	}
	if err := st.AcknowledgeSessionRotationClaim(claim.ClaimID, newThread); err != nil {
		t.Fatal(err)
	}
	issued, err := st.LoadSessionRotationMarker(oldThread)
	if err != nil {
		t.Fatal(err)
	}
	if issued.State != SessionRotationStateIssued || issued.Issued == nil || issued.Issued.BoundThreadID != newThread {
		t.Fatalf("旧thread marker = %#v", issued)
	}
	if issued.Directive == nil {
		t.Fatalf("issued markerがdirective記録を失いました: %#v", issued)
	}
	current, err := st.LoadSessionRotationMarker(newThread)
	if err != nil {
		t.Fatal(err)
	}
	if current.State != SessionRotationStatePending {
		t.Fatalf("bindしたthread自身のpending directiveが変化しました: %#v", current)
	}
}

func TestAcceptParentReviewRotationTransaction(t *testing.T) {
	seed := func(t *testing.T) *StateStore {
		t.Helper()
		st := &StateStore{dir: t.TempDir()}
		if _, err := st.StartNewTask(); err != nil {
			t.Fatal(err)
		}
		if err := st.SetTaskStatus(TaskStatusComplete); err != nil {
			t.Fatal(err)
		}
		st.UpdateTaskStats(func(stats *TaskStats) {
			stats.openParentReview("PASS", "LOW", ParentReviewProducer{})
		})
		return st
	}
	threadID := "01a0463c-d477-7410-9efd-cb34ff2e0b0e"

	t.Run("accept awaits without issuing rotation", func(t *testing.T) {
		st := seed(t)
		accepted, err := st.AcceptParentReview()
		if err != nil || !accepted {
			t.Fatalf("accept = %v err=%v", accepted, err)
		}
		if got := st.TaskStatus(); got != TaskStatusAwaitingParentCompletion {
			t.Fatalf("accept後のstatus = %q", got)
		}
		marker, err := st.LoadSessionRotationMarker(threadID)
		if err != nil || marker != nil {
			t.Fatalf("acceptがrotation markerを書きました: %#v err=%v", marker, err)
		}
	})

	t.Run("required decision writes marker inside completion", func(t *testing.T) {
		st := seed(t)
		if _, err := st.AcceptParentReview(); err != nil {
			t.Fatal(err)
		}
		completed, err := st.CompleteParentAwaiting(func(string) (*SessionRotationEvaluation, error) {
			return &SessionRotationEvaluation{
				ParentThreadID: threadID,
				TaskID:         st.ReadOr("task.id", ""),
				Terminal:       SessionRotationTerminalAccept,
				Decision:       SessionRotationDecision{Required: true, Reason: SessionRotationReasonCompaction},
			}, nil
		})
		if err != nil || !completed {
			t.Fatalf("completion = %v err=%v", completed, err)
		}
		marker, err := st.LoadSessionRotationMarker(threadID)
		if err != nil || marker == nil || marker.State != SessionRotationStatePending || marker.Directive == nil {
			t.Fatalf("completion後のmarker = %#v err=%v", marker, err)
		}
	})

	t.Run("evaluation failure rolls the terminal back", func(t *testing.T) {
		st := seed(t)
		if _, err := st.AcceptParentReview(); err != nil {
			t.Fatal(err)
		}
		before, err := st.CurrentTaskStats()
		if err != nil {
			t.Fatal(err)
		}
		_, err = st.CompleteParentAwaiting(func(string) (*SessionRotationEvaluation, error) {
			return nil, os.ErrPermission
		})
		if err == nil {
			t.Fatal("評価errorがcompletionをfail closedしませんでした")
		}
		if got := st.TaskStatus(); got != TaskStatusAwaitingParentCompletion {
			t.Fatalf("rollback後のstatus = %q", got)
		}
		after, err := st.CurrentTaskStats()
		if err != nil {
			t.Fatal(err)
		}
		if after.Status != TaskStatusAwaitingParentCompletion || after.ParentOutcomes[ParentOutcomeAccepted] != 1 {
			t.Fatalf("rollback後のstats = %#v (before=%#v)", after, before)
		}
		retry, err := st.CompleteParentAwaiting(func(string) (*SessionRotationEvaluation, error) {
			return &SessionRotationEvaluation{
				ParentThreadID: threadID,
				TaskID:         st.ReadOr("task.id", ""),
				Terminal:       SessionRotationTerminalAccept,
				Decision:       SessionRotationDecision{Required: false},
			}, nil
		})
		if err != nil || !retry {
			t.Fatalf("再試行completion = %v err=%v", retry, err)
		}
		marker, err := st.LoadSessionRotationMarker(threadID)
		if err != nil {
			t.Fatal(err)
		}
		if marker == nil || marker.State != "" || marker.LastEvaluation == nil || marker.LastEvaluation.Required {
			t.Fatalf("not-required completion後のmarker = %#v", marker)
		}
	})
}

func TestProjectSessionRotationStates(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	unbound, err := st.ProjectSessionRotation("")
	if err != nil || unbound.State != SessionRotationProjectionUnavailable || unbound.Reason == "" {
		t.Fatalf("unbound projection = %#v err=%v", unbound, err)
	}
	threadID := "01a0463c-d477-7410-9efd-cb34ff2e0b0e"
	absent, err := st.ProjectSessionRotation(threadID)
	if err != nil || absent.State != SessionRotationProjectionUnavailable {
		t.Fatalf("absent projection = %#v err=%v", absent, err)
	}
	if err := st.commitSessionRotation(&SessionRotationEvaluation{
		ParentThreadID: threadID,
		TaskID:         "12345678-aaaa-bbbb-cccc-dddddddddddd",
		Terminal:       SessionRotationTerminalAccept,
		Decision:       SessionRotationDecision{Required: true, Reason: SessionRotationReasonModelTurns},
	}); err != nil {
		t.Fatal(err)
	}
	pending, err := st.ProjectSessionRotation(threadID)
	if err != nil || pending.State != SessionRotationProjectionPending || pending.Directive == nil {
		t.Fatalf("pending projection = %#v err=%v", pending, err)
	}
	if pending.Directive.Reason != SessionRotationReasonModelTurns {
		t.Fatalf("pending directive = %#v", pending.Directive)
	}
	newThread := "01a0244a-4ee4-7e71-b2e1-dec3bdda2120"
	marker, err := st.LoadSessionRotationMarker(threadID)
	if err != nil {
		t.Fatal(err)
	}
	claim, err := st.ClaimSessionRotation(threadID, marker.Directive.DirectiveID)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := st.ProjectSessionRotation(threadID)
	if err != nil || claimed.State != SessionRotationStateClaimed || claimed.Claim == nil {
		t.Fatalf("claimed projection = %#v err=%v", claimed, err)
	}
	if err := st.BindSessionRotationClaim(threadID, marker.Directive.DirectiveID, claim.ClaimID, newThread); err != nil {
		t.Fatal(err)
	}
	bound, err := st.ProjectSessionRotation(threadID)
	if err != nil || bound.State != SessionRotationStateBound || bound.Claim.BoundThreadID != newThread {
		t.Fatalf("bound projection = %#v err=%v", bound, err)
	}
	if _, err := st.StartSessionRotationTask(newThread, claim.ClaimID); err != nil {
		t.Fatal(err)
	}
	if err := st.SetParentCodexIdentity(newThread, newThread, nil); err != nil {
		t.Fatal(err)
	}
	if err := st.AcknowledgeSessionRotationClaim(claim.ClaimID, newThread); err != nil {
		t.Fatal(err)
	}
	issued, err := st.ProjectSessionRotation(threadID)
	if err != nil || issued.State != SessionRotationProjectionNotRequired || issued.Directive != nil {
		t.Fatalf("issued projection = %#v err=%v", issued, err)
	}
	if _, err := st.ProjectSessionRotation("bad"); err == nil {
		t.Fatal("invalid thread identityがfail closedしませんでした")
	}
}

func TestSessionRotationCreationFailureReleaseAndRetry(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	oldThread := "01a0463c-d477-7410-9efd-cb34ff2e0b0e"
	if err := st.commitSessionRotation(&SessionRotationEvaluation{
		ParentThreadID: oldThread, TaskID: "12345678-aaaa-bbbb-cccc-dddddddddddd", Terminal: SessionRotationTerminalAccept,
		Decision: SessionRotationDecision{Required: true, Reason: SessionRotationReasonCompaction},
	}); err != nil {
		t.Fatal(err)
	}
	marker, _ := st.LoadSessionRotationMarker(oldThread)
	first, err := st.ClaimSessionRotation(oldThread, marker.Directive.DirectiveID)
	if err != nil {
		t.Fatal(err)
	}
	idempotent, err := st.ClaimSessionRotation(oldThread, marker.Directive.DirectiveID)
	if err != nil || idempotent.ClaimID != first.ClaimID || idempotent.TargetTaskID != first.TargetTaskID {
		t.Fatalf("idempotent claim = %#v err=%v", idempotent, err)
	}
	if err := st.ReleaseSessionRotationClaim(oldThread, marker.Directive.DirectiveID, first.ClaimID, sessionRotationFailedCreationResult(oldThread, marker.Directive.DirectiveID, first)); err != nil {
		t.Fatal(err)
	}
	retry, err := st.ClaimSessionRotation(oldThread, marker.Directive.DirectiveID)
	if err != nil || retry.ClaimID == first.ClaimID {
		t.Fatalf("retry claim = %#v err=%v", retry, err)
	}
	if err := st.ReleaseSessionRotationClaim(oldThread, marker.Directive.DirectiveID, retry.ClaimID, sessionRotationFailedCreationResult(oldThread, marker.Directive.DirectiveID, retry)); err != nil {
		t.Fatal(err)
	}
}

func TestSessionRotationStartAcceptsLatestEvaluationAfterLaterCompletedTask(t *testing.T) {
	seed := seedBoundRotationAfterLaterTask(t, true, TaskStatusComplete)
	resume, err := seed.st.AdmitNewTaskRotation(seed.newThread, seed.claim.ClaimID)
	if err != nil || resume {
		t.Fatalf("latest evaluation admission = resume:%v err:%v", resume, err)
	}
	if err := seed.st.ValidateNewTaskRotation(seed.newThread, seed.claim.ClaimID); err != nil {
		t.Fatalf("latest terminal evaluationからのstart admissionが拒否されました: %v", err)
	}
	started, err := seed.st.StartSessionRotationTask(seed.newThread, seed.claim.ClaimID)
	if err != nil || started != seed.claim.TargetTaskID {
		t.Fatalf("rotation start = %s err=%v", started, err)
	}
	if current := seed.st.ReadOr("task.id", ""); current != seed.claim.TargetTaskID {
		t.Fatalf("started task = %s", current)
	}
	if err := seed.st.SetParentCodexIdentity(seed.newThread, seed.newThread, nil); err != nil {
		t.Fatal(err)
	}
	if err := seed.st.AcknowledgeSessionRotationClaim(seed.claim.ClaimID, seed.newThread); err != nil {
		t.Fatal(err)
	}
	issued, err := seed.st.LoadSessionRotationMarker(seed.oldThread)
	if err != nil || issued.State != SessionRotationStateIssued || issued.Issued == nil || issued.Issued.BoundThreadID != seed.newThread {
		t.Fatalf("issued marker = %#v err=%v", issued, err)
	}
}

func seedBoundRotationAfterLaterTask(t *testing.T, laterRequired bool, laterStatus TaskStatus) boundRotationAfterLaterTaskSeed {
	t.Helper()
	st := &StateStore{dir: t.TempDir()}
	oldThread := "01a0463c-d477-7410-9efd-cb34ff2e0b0e"
	newThread := "01a0244a-4ee4-7e71-b2e1-dec3bdda2120"
	directiveTask, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.commitSessionRotation(&SessionRotationEvaluation{
		ParentThreadID: oldThread, TaskID: directiveTask, Terminal: SessionRotationTerminalAccept,
		Decision: SessionRotationDecision{Required: true, Reason: SessionRotationReasonDefaultTwoTasks},
	}); err != nil {
		t.Fatal(err)
	}
	laterTask, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(laterStatus); err != nil {
		t.Fatal(err)
	}
	if err := st.commitSessionRotation(&SessionRotationEvaluation{
		ParentThreadID: oldThread, TaskID: laterTask, Terminal: SessionRotationTerminalAccept,
		Decision: SessionRotationDecision{Required: laterRequired, Reason: SessionRotationReasonDefaultTwoTasks},
	}); err != nil {
		t.Fatal(err)
	}
	marker, err := st.LoadSessionRotationMarker(oldThread)
	if err != nil {
		t.Fatal(err)
	}
	claim, err := st.ClaimSessionRotation(oldThread, marker.Directive.DirectiveID)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.BindSessionRotationClaim(oldThread, marker.Directive.DirectiveID, claim.ClaimID, newThread); err != nil {
		t.Fatal(err)
	}
	return boundRotationAfterLaterTaskSeed{st: st, oldThread: oldThread, newThread: newThread, claim: claim}
}

func TestSessionRotationLatestEvaluationAdmissionFailsClosed(t *testing.T) {
	t.Run("active later task is not a rotation source", func(t *testing.T) {
		seed := seedBoundRotationAfterLaterTask(t, true, TaskStatusActive)
		if err := seed.st.ValidateNewTaskRotation(seed.newThread, seed.claim.ClaimID); err == nil {
			t.Fatal("未完了taskをrotation元としたstartが受理されました")
		}
	})
	t.Run("not-required evaluation is not a rotation source", func(t *testing.T) {
		seed := seedBoundRotationAfterLaterTask(t, false, TaskStatusComplete)
		if err := seed.st.ValidateNewTaskRotation(seed.newThread, seed.claim.ClaimID); err == nil {
			t.Fatal("not-required evaluationをrotation元としたstartが受理されました")
		}
	})
	t.Run("task newer than the evaluation fails closed", func(t *testing.T) {
		seed := seedBoundRotationAfterLaterTask(t, true, TaskStatusComplete)
		if _, err := seed.st.StartNewTask(); err != nil {
			t.Fatal(err)
		}
		if err := seed.st.SetTaskStatus(TaskStatusComplete); err != nil {
			t.Fatal(err)
		}
		if err := seed.st.ValidateNewTaskRotation(seed.newThread, seed.claim.ClaimID); err == nil {
			t.Fatal("最新evaluationと一致しないtaskからのstartが受理されました")
		}
		if _, err := seed.st.StartSessionRotationTask(seed.newThread, seed.claim.ClaimID); err == nil {
			t.Fatal("最新evaluationと一致しないtaskからのstart実行が受理されました")
		}
	})
	t.Run("unknown terminal evaluation fails closed", func(t *testing.T) {
		seed := seedBoundRotationAfterLaterTask(t, true, TaskStatusComplete)
		marker, err := seed.st.LoadSessionRotationMarker(seed.oldThread)
		if err != nil {
			t.Fatal(err)
		}
		marker.LastEvaluation.Terminal = "park"
		if err := seed.st.writeSessionRotationMarker(marker); err != nil {
			t.Fatal(err)
		}
		if err := seed.st.ValidateNewTaskRotation(seed.newThread, seed.claim.ClaimID); err == nil {
			t.Fatal("未知terminal評価をrotation元としたstartが受理されました")
		}
		if _, err := seed.st.StartSessionRotationTask(seed.newThread, seed.claim.ClaimID); err == nil {
			t.Fatal("未知terminal評価からのstart実行が受理されました")
		}
	})
}

func TestSessionRotationStaleDirectiveBoundClaimRetryFixture(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	parentThread := "01a07e12-7651-76a0-92d4-c244e4963f62"
	boundThread := "01a08268-2c7d-72a0-9aa1-605eed066ce9"
	directiveTask := "38bc5f3b-e938-4540-a798-7af2e506f085"
	completedTask := "7fefc2cd-48a0-4887-a679-50978a0ae237"
	claimID := "9115ac5c-d4e1-48b2-8f48-e79a553d396f"
	targetTask := "cffc13b4-f630-43e1-a2d8-aa3d8103b34d"
	marker := &SessionRotationMarker{
		Version:        sessionRotationMarkerVersion,
		ParentThreadID: parentThread,
		State:          SessionRotationStateBound,
		Directive: &SessionRotationDirective{
			DirectiveID: "84e3f02e-cf23-4cc8-aaee-f3fb42aad71e",
			TaskID:      directiveTask,
			Terminal:    SessionRotationTerminalAccept,
			Epoch:       directiveTask + ":" + SessionRotationTerminalAccept,
			Reason:      SessionRotationReasonDefaultTwoTasks,
			Evidence:    []SessionRotationEvidence{{Trigger: SessionRotationReasonDefaultTwoTasks, Field: "accepted_tasks", Value: "2"}},
			CreatedAt:   "2026-09-08T00:00:00Z",
		},
		Claim: &SessionRotationClaim{
			ClaimID:          claimID,
			ClaimantThreadID: parentThread,
			TargetTaskID:     targetTask,
			ClaimedAt:        "2026-09-08T00:00:00Z",
			BoundThreadID:    boundThread,
			BoundAt:          "2026-09-08T00:00:00Z",
		},
		LastEvaluation: &SessionRotationEvaluationRecord{
			TaskID:   completedTask,
			Terminal: SessionRotationTerminalAccept,
			Required: true,
			Reason:   SessionRotationReasonDefaultTwoTasks,
			At:       "2026-09-08T00:00:00Z",
		},
	}
	if err := st.writeSessionRotationMarker(marker); err != nil {
		t.Fatal(err)
	}
	if _, err := st.startNewTaskWithID(completedTask, false); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(TaskStatusComplete); err != nil {
		t.Fatal(err)
	}
	if err := st.ValidateNewTaskRotation(boundThread, claimID); err != nil {
		t.Fatalf("現行bound claimと同型fixtureのstart admissionが拒否されました: %v", err)
	}
	started, err := st.StartSessionRotationTask(boundThread, claimID)
	if err != nil || started != targetTask {
		t.Fatalf("retry start = %s err=%v", started, err)
	}
	if current := st.ReadOr("task.id", ""); current != targetTask {
		t.Fatalf("started task = %s", current)
	}
	if err := st.SetParentCodexIdentity(boundThread, boundThread, nil); err != nil {
		t.Fatal(err)
	}
	if err := st.AcknowledgeSessionRotationClaim(claimID, boundThread); err != nil {
		t.Fatal(err)
	}
	issued, err := st.LoadSessionRotationMarker(parentThread)
	if err != nil || issued.State != SessionRotationStateIssued || issued.Issued == nil || issued.Issued.BoundThreadID != boundThread {
		t.Fatalf("issued marker = %#v err=%v", issued, err)
	}
}

func TestValidateNewTaskRotationPreservesNormalPathAndRejectsOldThread(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	oldThread := "01a0463c-d477-7410-9efd-cb34ff2e0b0e"
	newThread := "01a0244a-4ee4-7e71-b2e1-dec3bdda2120"
	if err := st.SetParentCodexIdentity(oldThread, oldThread, nil); err != nil {
		t.Fatal(err)
	}
	if err := st.ValidateNewTaskRotation(oldThread, ""); err != nil {
		t.Fatalf("通常new-task pathが拒否されました: %v", err)
	}
	if err := st.commitSessionRotation(&SessionRotationEvaluation{
		ParentThreadID: oldThread, TaskID: st.ReadOr("task.id", ""), Terminal: SessionRotationTerminalAccept,
		Decision: SessionRotationDecision{Required: true, Reason: SessionRotationReasonCompaction},
	}); err != nil {
		t.Fatal(err)
	}
	marker, _ := st.LoadSessionRotationMarker(oldThread)
	if err := st.ValidateNewTaskRotation(oldThread, ""); err == nil {
		t.Fatal("pending directive中に旧threadのstartが受理されました")
	}
	claim, err := st.ClaimSessionRotation(oldThread, marker.Directive.DirectiveID)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.BindSessionRotationClaim(oldThread, marker.Directive.DirectiveID, claim.ClaimID, newThread); err != nil {
		t.Fatal(err)
	}
	if err := st.ValidateNewTaskRotation(oldThread, claim.ClaimID); err == nil {
		t.Fatal("claimを旧threadが使用できました")
	}
	if err := st.ValidateNewTaskRotation(newThread, claim.ClaimID); err != nil {
		t.Fatalf("bound new threadが拒否されました: %v", err)
	}
}

func TestSessionRotationIssuedStartRetriesOnlyFromActiveCheckpoint(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	oldThread := "01a0463c-d477-7410-9efd-cb34ff2e0b0e"
	newThread := "01a0244a-4ee4-7e71-b2e1-dec3bdda2120"
	if err := st.SetParentCodexIdentity(oldThread, oldThread, nil); err != nil {
		t.Fatal(err)
	}
	if err := st.commitSessionRotation(&SessionRotationEvaluation{
		ParentThreadID: oldThread, TaskID: st.ReadOr("task.id", ""), Terminal: SessionRotationTerminalAccept,
		Decision: SessionRotationDecision{Required: true, Reason: SessionRotationReasonCompaction},
	}); err != nil {
		t.Fatal(err)
	}
	marker, _ := st.LoadSessionRotationMarker(oldThread)
	claim, err := st.ClaimSessionRotation(oldThread, marker.Directive.DirectiveID)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.BindSessionRotationClaim(oldThread, marker.Directive.DirectiveID, claim.ClaimID, newThread); err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartSessionRotationTask(newThread, claim.ClaimID); err != nil {
		t.Fatal(err)
	}
	if err := st.SetParentCodexIdentity(newThread, newThread, nil); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveResumeCheckpoint(ResumeCheckpoint{Stage: ResumeStageWorker, Phase: "worker-new", Role: WorkerRole, Model: "opus"}); err != nil {
		t.Fatal(err)
	}
	if err := st.AcknowledgeSessionRotationClaim(claim.ClaimID, newThread); err != nil {
		t.Fatal(err)
	}
	if resume, err := st.AdmitNewTaskRotation(newThread, claim.ClaimID); err != nil || !resume {
		t.Fatalf("active checkpoint retry = %v, %v", resume, err)
	}
	if _, err := st.AdmitNewTaskRotation(newThread, ""); err == nil {
		t.Fatal("active rotated task accepted retry without claim")
	}
	if err := st.SetTaskStatus(TaskStatusComplete); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AdmitNewTaskRotation(newThread, claim.ClaimID); err == nil {
		t.Fatal("completed rotated task accepted duplicate start")
	}
}

func TestAdmitNewTaskRotationSkipsVanishedRotationMarker(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	vanished := "01a0463c-d477-7410-9efd-cb34ff2e0b0e"
	if err := os.MkdirAll(st.Path(sessionRotationDirectory), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(st.Path(sessionRotationDirectory), "missing.json"), st.SessionRotationMarkerPath(vanished)); err != nil {
		t.Fatal(err)
	}
	admitted, err := st.AdmitNewTaskRotation("01a0244a-4ee4-7e71-b2e1-dec3bdda2120", "")
	if err != nil || admitted {
		t.Fatalf("消失marker列入時のadmission = %v, %v", admitted, err)
	}
}
