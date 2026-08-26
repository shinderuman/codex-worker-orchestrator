package autoresume

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

const wakeResetMargin = 2 * time.Minute

const (
	wakeActionNotifyParent actionKind = "notify_parent"
	wakeActionFetchReset   actionKind = "fetch_reset"
	wakeActionPause        actionKind = "pause"
)

type wakeEnv interface {
	NotifyParent() error
	FetchReset() (time.Time, error)
	UpdateActive(id string) string
	PauseAutomation(id string) error
	VerifyReservation() verification
}

type wakeResult struct {
	Reserved      bool
	Actions       []action
	FailureReason string
	ManualID      string
}

type scriptedWakeEnv struct {
	resetResponses  []string
	updateResponses []string
	pauseErrors     []string
	verifications   []verification
	notifyError     string
	fetchCalls      int
	updateCalls     int
	pauseCalls      int
	verifyCalls     int
	fetchedAt       []time.Time
	t               *testing.T
}

func (e *scriptedWakeEnv) NotifyParent() error {
	e.t.Helper()
	if e.notifyError != "" {
		return errors.New(e.notifyError)
	}
	return nil
}

func (e *scriptedWakeEnv) FetchReset() (time.Time, error) {
	e.t.Helper()
	e.fetchCalls++
	if e.fetchCalls > len(e.resetResponses) {
		e.t.Fatalf("unexpected FetchReset call %d", e.fetchCalls)
	}
	raw := e.resetResponses[e.fetchCalls-1]
	if raw == "" {
		return time.Time{}, errors.New("codex-limit reset fetch failed")
	}
	at, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		e.t.Fatalf("scenario reset response %q is not RFC3339: %v", raw, err)
	}
	e.fetchedAt = append(e.fetchedAt, at)
	return at, nil
}

func (e *scriptedWakeEnv) UpdateActive(id string) string {
	e.t.Helper()
	e.updateCalls++
	if e.updateCalls > len(e.updateResponses) {
		e.t.Fatalf("unexpected UpdateActive call %d", e.updateCalls)
	}
	return e.updateResponses[e.updateCalls-1]
}

func (e *scriptedWakeEnv) PauseAutomation(id string) error {
	e.t.Helper()
	e.pauseCalls++
	if e.pauseCalls > len(e.pauseErrors) {
		e.t.Fatalf("unexpected PauseAutomation call %d", e.pauseCalls)
	}
	if text := e.pauseErrors[e.pauseCalls-1]; text != "" {
		return errors.New(text)
	}
	return nil
}

func (e *scriptedWakeEnv) VerifyReservation() verification {
	e.t.Helper()
	e.verifyCalls++
	if e.verifyCalls > len(e.verifications) {
		e.t.Fatalf("unexpected VerifyReservation call %d", e.verifyCalls)
	}
	return e.verifications[e.verifyCalls-1]
}

func orchestrateWakeReschedule(env wakeEnv, parentThreadSpecified bool, firedAutomationID string, now time.Time) wakeResult {
	actions := []action{}
	if !parentThreadSpecified {
		return wakeFailure(env, actions, firedAutomationID, "parent thread ID is not specified")
	}
	actions = append(actions, action{Kind: wakeActionNotifyParent})
	if err := env.NotifyParent(); err != nil {
		return wakeFailure(env, actions, firedAutomationID, fmt.Sprintf("parent notify failed: %v", err))
	}
	actions, resetAt, ok := fetchWakeReset(env, actions, now)
	if !ok {
		return wakeFailure(env, actions, firedAutomationID, "codex-limit did not return a future resets_at after one refetch")
	}
	if firedAutomationID == "" {
		return wakeFailure(env, actions, "", "fired automation_id is missing from the heartbeat instruction")
	}
	wakeAt := resetAt.Add(wakeResetMargin)
	dtStart := wakeAt.UTC().Format(dtStartLayout)
	actions, reserved, reason := updateAndVerify(env, firedAutomationID, dtStart, actions)
	if reserved {
		actions = append(actions, action{Kind: actionReportReserved})
		return wakeResult{Reserved: true, Actions: actions}
	}
	return wakeFailure(env, actions, firedAutomationID, reason)
}

func fetchWakeReset(env wakeEnv, actions []action, now time.Time) ([]action, time.Time, bool) {
	actions = append(actions, action{Kind: wakeActionFetchReset})
	resetAt, err := env.FetchReset()
	if err != nil {
		return actions, time.Time{}, false
	}
	if resetAt.After(now) {
		return actions, resetAt, true
	}
	actions = append(actions, action{Kind: wakeActionFetchReset})
	resetAt, err = env.FetchReset()
	if err != nil || !resetAt.After(now) {
		return actions, time.Time{}, false
	}
	return actions, resetAt, true
}

func wakeFailure(env wakeEnv, actions []action, automationID, reason string) wakeResult {
	if automationID != "" {
		actions = append(actions, action{Kind: wakeActionPause, AutomationID: automationID})
		if err := env.PauseAutomation(automationID); err != nil {
			actions = append(actions, action{Kind: actionReportFailure})
			return wakeResult{Actions: actions, FailureReason: reason + "; pause also failed: " + err.Error(), ManualID: automationID}
		}
	}
	actions = append(actions, action{Kind: actionReportFailure})
	return wakeResult{Actions: actions, FailureReason: reason}
}
