package state

import "testing"

func TestSessionRotationAcceptedTaskCountUsesDurableMarkerCount(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	threadID := "01a0463c-d477-7410-9efd-cb34ff2e0b0e"
	count := 1
	marker := &SessionRotationMarker{Version: sessionRotationMarkerVersion, ParentThreadID: threadID, AcceptedTasks: &count}
	if err := st.writeSessionRotationMarker(marker); err != nil {
		t.Fatal(err)
	}
	got, available, source, err := st.SessionRotationAcceptedTaskCount(threadID)
	if err != nil {
		t.Fatal(err)
	}
	if !available || got != 1 || source != st.SessionRotationMarkerPath(threadID) {
		t.Fatalf("accepted-task state = count:%d available:%v source:%q", got, available, source)
	}
}

func TestSessionRotationAcceptedTaskCountBootstrapsIssuedTargetAtZero(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	sourceThread := "01a0463c-d477-7410-9efd-cb34ff2e0b0e"
	targetThread := "01a0463c-d477-7410-9efd-cb34ff2e0b0f"
	directiveID, err := NewUUID()
	if err != nil {
		t.Fatal(err)
	}
	source := &SessionRotationMarker{
		Version:        sessionRotationMarkerVersion,
		ParentThreadID: sourceThread,
		State:          SessionRotationStateIssued,
		Directive: &SessionRotationDirective{
			DirectiveID: directiveID,
			TaskID:      "12345678-aaaa-bbbb-cccc-ddddddddddde",
			Terminal:    SessionRotationTerminalAccept,
			Epoch:       "12345678-aaaa-bbbb-cccc-ddddddddddde:accept",
			Reason:      SessionRotationReasonDefaultTwoTasks,
			Evidence:    []SessionRotationEvidence{},
			CreatedAt:   "2026-09-12T00:00:00Z",
		},
		Issued: &SessionRotationIssued{BoundThreadID: targetThread, IssuedAt: "2026-09-12T00:01:00Z"},
	}
	if err := st.writeSessionRotationMarker(source); err != nil {
		t.Fatal(err)
	}
	got, available, evidenceSource, err := st.SessionRotationAcceptedTaskCount(targetThread)
	if err != nil {
		t.Fatal(err)
	}
	if !available || got != 0 || evidenceSource != st.SessionRotationMarkerPath(sourceThread) {
		t.Fatalf("accepted-task state = count:%d available:%v source:%q", got, available, evidenceSource)
	}
}

func TestSessionRotationAcceptedTaskCountDoesNotInferZeroForUnknownHistory(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	threadID := "01a0463c-d477-7410-9efd-cb34ff2e0b0e"
	got, available, source, err := st.SessionRotationAcceptedTaskCount(threadID)
	if err != nil {
		t.Fatal(err)
	}
	if available || got != 0 || source != st.SessionRotationMarkerPath(threadID) {
		t.Fatalf("accepted-task state = count:%d available:%v source:%q", got, available, source)
	}
}

func TestSessionRotationAcceptedTaskCountDoesNotBootstrapMarkerWithExistingTaskHistory(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	sourceThread := "01a0463c-d477-7410-9efd-cb34ff2e0b0e"
	targetThread := "01a0463c-d477-7410-9efd-cb34ff2e0b0f"
	directiveID, err := NewUUID()
	if err != nil {
		t.Fatal(err)
	}
	source := &SessionRotationMarker{
		Version:        sessionRotationMarkerVersion,
		ParentThreadID: sourceThread,
		State:          SessionRotationStateIssued,
		Directive: &SessionRotationDirective{
			DirectiveID: directiveID,
			TaskID:      "12345678-aaaa-bbbb-cccc-ddddddddddde",
			Terminal:    SessionRotationTerminalAccept,
			Epoch:       "12345678-aaaa-bbbb-cccc-ddddddddddde:accept",
			Reason:      SessionRotationReasonDefaultTwoTasks,
			Evidence:    []SessionRotationEvidence{},
			CreatedAt:   "2026-09-12T00:00:00Z",
		},
		Issued: &SessionRotationIssued{BoundThreadID: targetThread, IssuedAt: "2026-09-12T00:01:00Z"},
	}
	if err := st.writeSessionRotationMarker(source); err != nil {
		t.Fatal(err)
	}
	target := &SessionRotationMarker{
		Version:        sessionRotationMarkerVersion,
		ParentThreadID: targetThread,
		LastEvaluation: &SessionRotationEvaluationRecord{
			TaskID: "12345678-aaaa-bbbb-cccc-dddddddddddf", Terminal: SessionRotationTerminalAccept, At: "2026-09-12T00:02:00Z",
		},
	}
	if err := st.writeSessionRotationMarker(target); err != nil {
		t.Fatal(err)
	}
	got, available, evidenceSource, err := st.SessionRotationAcceptedTaskCount(targetThread)
	if err != nil {
		t.Fatal(err)
	}
	if available || got != 0 || evidenceSource != st.SessionRotationMarkerPath(targetThread) {
		t.Fatalf("accepted-task state = count:%d available:%v source:%q", got, available, evidenceSource)
	}
}

func TestCommitSessionRotationPersistsAcceptedTaskCount(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	threadID := "01a0463c-d477-7410-9efd-cb34ff2e0b0e"
	evaluation := &SessionRotationEvaluation{
		ParentThreadID: threadID,
		TaskID:         "12345678-aaaa-bbbb-cccc-dddddddddddd",
		Terminal:       SessionRotationTerminalAccept,
		AcceptedTasks:  1,
		Decision:       SessionRotationDecision{Evidence: []SessionRotationEvidence{}},
	}
	if err := st.commitSessionRotation(evaluation); err != nil {
		t.Fatal(err)
	}
	marker, err := st.LoadSessionRotationMarker(threadID)
	if err != nil {
		t.Fatal(err)
	}
	if marker == nil || marker.AcceptedTasks == nil || *marker.AcceptedTasks != 1 {
		t.Fatalf("accepted task count was not persisted: %#v", marker)
	}
}

func TestCommitSessionRotationPreservesUnknownAcceptedTaskHistory(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	threadID := "01a0463c-d477-7410-9efd-cb34ff2e0b0e"
	evaluation := &SessionRotationEvaluation{
		ParentThreadID:           threadID,
		TaskID:                   "12345678-aaaa-bbbb-cccc-dddddddddddd",
		Terminal:                 SessionRotationTerminalAccept,
		AcceptedTasksUnavailable: true,
		Decision:                 SessionRotationDecision{Evidence: []SessionRotationEvidence{}},
	}
	if err := st.commitSessionRotation(evaluation); err != nil {
		t.Fatal(err)
	}
	marker, err := st.LoadSessionRotationMarker(threadID)
	if err != nil {
		t.Fatal(err)
	}
	if marker == nil || marker.AcceptedTasks != nil {
		t.Fatalf("unknown accepted task history was converted into a count: %#v", marker)
	}
}

func TestSessionRotationDefaultTriggerCarriesAcceptedTaskProvenance(t *testing.T) {
	source := "/state/rotation/thread.json"
	decision := DecideSessionRotation(SessionRotationSignals{
		Terminal:                SessionRotationTerminalAccept,
		AcceptedTasks:           2,
		AcceptedTasksSource:     source,
		Rollout:                 &SessionRotationRolloutSignals{},
		MaterialEventsAvailable: true,
		Limit:                   &SessionRotationLimitSignals{},
	})
	if !decision.Required || decision.Reason != SessionRotationReasonDefaultTwoTasks || len(decision.Evidence) != 1 {
		t.Fatalf("decision = %#v", decision)
	}
	if decision.Evidence[0].Field != SessionRotationEvidenceFieldAcceptedTasks || decision.Evidence[0].Value != "2" || decision.Evidence[0].Source != source {
		t.Fatalf("accepted-task evidence = %#v", decision.Evidence[0])
	}
}
