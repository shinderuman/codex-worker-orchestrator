package autoresume

import (
	"fmt"
	"regexp"
	"strings"
)

type actionKind string

const (
	actionCreatePlaceholder actionKind = "create_placeholder"
	actionUpdateActive      actionKind = "update_active"
	actionDelete            actionKind = "delete"
	actionVerify            actionKind = "verify"
	actionReportReserved    actionKind = "report_reserved"
	actionReportFailure     actionKind = "report_failure"
)

const placeholderRrule = "RRULE:FREQ=HOURLY"

type action struct {
	Kind         actionKind
	AutomationID string
	DTStart      string
	Rrule        string
	Status       string
}

func (a action) String() string {
	if a.AutomationID != "" {
		return string(a.Kind) + ":" + a.AutomationID
	}
	return string(a.Kind)
}

type responseClass int

const (
	responseSuccess responseClass = iota
	responseFailure
)

type verification struct {
	Outcome            Outcome
	ManualAppConfirmed bool
}

type reservationEnv interface {
	CreatePlaceholder(key string) string
	UpdateActive(id string) string
	DeleteAutomation(id string) error
	VerifyReservation() verification
}

type updateVerifyEnv interface {
	UpdateActive(id string) string
	VerifyReservation() verification
}

type reservationResult struct {
	Reserved         bool
	Actions          []action
	FailureReason    string
	ManualFallbackID string
}

var automationIDPattern = regexp.MustCompile(`(?i)automation[\s_-]*id\s*[:=]\s*"?([A-Za-z0-9][A-Za-z0-9_.-]*)"?`)

func classifyResponse(text string) (responseClass, string) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return responseFailure, ""
	}
	lower := strings.ToLower(trimmed)
	for _, marker := range []string{"invalid", "error", "failed", "rendered suggestion", "suggested_create"} {
		if strings.Contains(lower, marker) {
			return responseFailure, ""
		}
	}
	explicit := false
	for _, marker := range []string{"created automation in the app", "updated automation in the app", "success"} {
		if strings.Contains(lower, marker) {
			explicit = true
			break
		}
	}
	if !explicit {
		return responseFailure, ""
	}
	m := automationIDPattern.FindStringSubmatch(trimmed)
	if m == nil {
		return responseFailure, ""
	}
	return responseSuccess, m[1]
}

func orchestrateReservation(env reservationEnv, key, resumeAtRFC3339, existingAutomationID string) reservationResult {
	_, dtStart, _, err := expectedFromRFC3339(resumeAtRFC3339)
	if err != nil {
		return reservationFailure(nil, fmt.Sprintf("invalid resume time %q: %v", resumeAtRFC3339, err), "")
	}

	id := existingAutomationID
	created := false
	actions := []action{}
	if id == "" {
		actions = append(actions, action{Kind: actionCreatePlaceholder, Status: "PAUSED", Rrule: placeholderRrule})
		response := env.CreatePlaceholder(key)
		class, responseID := classifyResponse(response)
		if class != responseSuccess {
			return reservationFailure(actions, fmt.Sprintf("placeholder create response is not an explicit success with automation ID: %q", response), "")
		}
		id = responseID
		created = true
	}

	actions, reserved, reason := updateAndVerify(env, id, dtStart, actions)
	if reserved {
		actions = append(actions, action{Kind: actionReportReserved})
		return reservationResult{Reserved: true, Actions: actions}
	}
	if !created {
		return reservationFailure(actions, reason, "")
	}
	actions = append(actions, action{Kind: actionDelete, AutomationID: id})
	if err := env.DeleteAutomation(id); err != nil {
		return reservationFailure(actions, reason+"; placeholder delete also failed: "+err.Error(), id)
	}
	return reservationFailure(actions, reason, "")
}

func updateAndVerify(env updateVerifyEnv, id, dtStart string, actions []action) ([]action, bool, string) {
	actions, ok, reason := applyUpdate(env, id, dtStart, actions)
	if !ok {
		return actions, false, reason
	}
	actions = append(actions, action{Kind: actionVerify})
	reserved, reason, isFail := verificationOutcome(env.VerifyReservation())
	if reserved || !isFail {
		return actions, reserved, reason
	}

	actions, ok, reason = applyUpdate(env, id, dtStart, actions)
	if !ok {
		return actions, false, reason
	}
	actions = append(actions, action{Kind: actionVerify})
	reserved, retryReason, _ := verificationOutcome(env.VerifyReservation())
	if reserved {
		return actions, true, ""
	}
	return actions, false, retryReason + " after one retry"
}

func verificationOutcome(v verification) (reserved bool, reason string, isFail bool) {
	switch v.Outcome {
	case Pass:
		return true, "", false
	case Unavailable:
		if v.ManualAppConfirmed {
			return true, "", false
		}
		return false, "verification_unavailable without Codex app confirmation", false
	default:
		return false, "verification_failed", true
	}
}

func applyUpdate(env updateVerifyEnv, id, dtStart string, actions []action) ([]action, bool, string) {
	actions = append(actions, action{
		Kind:         actionUpdateActive,
		AutomationID: id,
		DTStart:      dtStart,
		Rrule:        "DTSTART:" + dtStart + "\nRRULE:FREQ=DAILY;COUNT=1",
		Status:       "ACTIVE",
	})
	if class, _ := classifyResponse(env.UpdateActive(id)); class != responseSuccess {
		return actions, false, "automation update response is not an explicit success"
	}
	return actions, true, ""
}

func reservationFailure(actions []action, reason, fallbackID string) reservationResult {
	actions = append(actions, action{Kind: actionReportFailure})
	return reservationResult{Actions: actions, FailureReason: reason, ManualFallbackID: fallbackID}
}
