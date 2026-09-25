package parentactioncmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
)

type parentActionTerminalProjectionStats struct {
	BudgetBytes        int      `json:"budget_bytes"`
	RawBytes           int      `json:"raw_bytes"`
	ProjectedBytes     int      `json:"projected_bytes"`
	SavedBytes         int      `json:"saved_bytes"`
	DeduplicatedFields int      `json:"deduplicated_fields"`
	TerminalMode       string   `json:"terminal_mode"`
	HandoffMode        string   `json:"handoff_mode"`
	OmittedFields      []string `json:"omitted_fields,omitempty"`
	ProjectedFields    []string `json:"projected_fields,omitempty"`
	Overflow           bool     `json:"overflow"`
	ParentToolCalls    int      `json:"parent_tool_calls"`
	RecoveryCalls      int      `json:"recovery_calls"`
}

type parentActionTerminalProjectionError struct {
	stats parentActionTerminalProjectionStats
}

const (
	parentActionTerminalAuthorityBudgetBytes = 2 * 1024
	parentActionTerminalBudgetBytes          = packet.MaxPacketBytes + parentActionTerminalAuthorityBudgetBytes
	parentActionTerminalJSONLineBytes        = 1
)

func (e *parentActionTerminalProjectionError) Error() string {
	return fmt.Sprintf("parent action terminal projection exceeds budget: projected=%d budget=%d", e.stats.ProjectedBytes, e.stats.BudgetBytes)
}

func projectParentActionTerminalEnvelope(terminalJSON, handoffJSON json.RawMessage) (parentActionTerminalEnvelopePayload, error) {
	rawEnvelope := parentActionTerminalEnvelope(terminalJSON, handoffJSON)
	rawBytes, err := json.Marshal(rawEnvelope)
	if err != nil {
		return parentActionTerminalEnvelopePayload{}, fmt.Errorf("marshal raw parent action terminal envelope: %w", err)
	}
	projectedHandoff, handoffOmitted, err := projectTerminalHandoff(handoffJSON, false)
	if err != nil {
		return parentActionTerminalEnvelopePayload{}, err
	}
	stats := parentActionTerminalProjectionStats{
		BudgetBytes:     parentActionTerminalBudgetBytes,
		RawBytes:        encodedJSONLineBytes(rawBytes),
		TerminalMode:    "full",
		HandoffMode:     "bounded",
		OmittedFields:   prefixedFields("handoff", handoffOmitted),
		ParentToolCalls: 1,
		RecoveryCalls:   0,
	}
	candidate := parentActionTerminalEnvelopePayload{
		Status:     "parent_action_terminal",
		Terminal:   terminalJSON,
		Handoff:    projectedHandoff,
		Projection: &stats,
	}
	return fitSemanticTerminalProjection(candidate, stats, terminalJSON)
}

func projectParentActionTerminalEnvelopeMode(terminalJSON, handoffJSON json.RawMessage, recovery bool) (parentActionTerminalEnvelopePayload, error) {
	envelope, err := projectParentActionTerminalEnvelope(terminalJSON, handoffJSON)
	if !recovery {
		return envelope, err
	}
	if err != nil {
		var projectionErr *parentActionTerminalProjectionError
		if errors.As(err, &projectionErr) {
			projectionErr.stats.RecoveryCalls = 1
			projectionErr.stats.HandoffMode = "recovery-bounded"
		}
		return envelope, err
	}
	if envelope.Projection == nil {
		return envelope, nil
	}
	stats := *envelope.Projection
	stats.RecoveryCalls = 1
	stats.HandoffMode = "recovery-bounded"
	envelope.Projection = &stats
	if err := finalizeProjectionStats(&envelope, &stats); err != nil {
		return parentActionTerminalEnvelopePayload{}, err
	}
	if stats.ProjectedBytes <= stats.BudgetBytes {
		return envelope, nil
	}
	return fitSemanticTerminalProjection(envelope, stats, terminalJSON)
}

func fitSemanticTerminalProjection(candidate parentActionTerminalEnvelopePayload, stats parentActionTerminalProjectionStats, terminalJSON json.RawMessage) (parentActionTerminalEnvelopePayload, error) {
	projectedTerminal, terminalMode, terminalOmitted, err := projectTerminalSemanticResult(terminalJSON)
	if err != nil {
		return parentActionTerminalEnvelopePayload{}, err
	}
	stats.TerminalMode = terminalMode
	stats.OmittedFields = sortedUniqueStrings(append(stats.OmittedFields, prefixedFields("terminal", terminalOmitted)...))
	candidate.Terminal = projectedTerminal
	candidate.Projection = &stats
	if err := finalizeProjectionStats(&candidate, &stats); err != nil {
		return parentActionTerminalEnvelopePayload{}, err
	}
	return fitRecoverableEvidenceProjection(candidate, stats)
}

func fitRecoverableEvidenceProjection(candidate parentActionTerminalEnvelopePayload, stats parentActionTerminalProjectionStats) (parentActionTerminalEnvelopePayload, error) {
	locatorProjected, projectedFields, changed, err := projectRecoverableTerminalEvidence(candidate.Terminal)
	if err != nil {
		return parentActionTerminalEnvelopePayload{}, err
	}
	if changed {
		stats.TerminalMode = "semantic-locators"
		stats.ProjectedFields = sortedUniqueStrings(append(stats.ProjectedFields, prefixedFields("terminal", projectedFields)...))
		candidate.Terminal = locatorProjected
		candidate.Projection = &stats
		if err := finalizeProjectionStats(&candidate, &stats); err != nil {
			return parentActionTerminalEnvelopePayload{}, err
		}
	}
	if stats.ProjectedBytes <= stats.BudgetBytes {
		return candidate, nil
	}
	stats.Overflow = true
	return parentActionTerminalEnvelopePayload{}, &parentActionTerminalProjectionError{stats: stats}
}

func writeTerminalProjectionFailurePayload(terminalJSON, handoffJSON json.RawMessage, projectionErr *parentActionTerminalProjectionError) (parentActionTerminalEnvelopePayload, error) {
	terminalIdentity, err := projectTerminalIdentity(terminalJSON)
	if err != nil {
		return parentActionTerminalEnvelopePayload{}, err
	}
	boundedHandoff, omitted, err := projectTerminalHandoff(handoffJSON, true)
	if err != nil {
		return parentActionTerminalEnvelopePayload{}, err
	}
	stats := projectionErr.stats
	stats.Overflow = true
	stats.TerminalMode = "identity-only"
	stats.HandoffMode = "overflow-minimal"
	stats.OmittedFields = sortedUniqueStrings(append(stats.OmittedFields, prefixedFields("handoff", omitted)...))
	payload := parentActionTerminalEnvelopePayload{
		Status:          "parent_action_terminal_projection_overflow",
		Terminal:        terminalIdentity,
		Handoff:         boundedHandoff,
		ProjectionError: projectionErr.Error(),
		Projection:      &stats,
	}
	if err := finalizeProjectionStats(&payload, &stats); err != nil {
		return parentActionTerminalEnvelopePayload{}, err
	}
	if stats.ProjectedBytes > stats.BudgetBytes {
		return parentActionTerminalEnvelopePayload{}, fmt.Errorf("projection overflow payload exceeds budget: projected=%d budget=%d", stats.ProjectedBytes, stats.BudgetBytes)
	}
	return payload, nil
}

func finalizeProjectionStats(payload *parentActionTerminalEnvelopePayload, stats *parentActionTerminalProjectionStats) error {
	stats.DeduplicatedFields = len(stats.OmittedFields)
	for iteration := 0; iteration < 3; iteration++ {
		raw, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("marshal projected parent action terminal envelope: %w", err)
		}
		projectedBytes := encodedJSONLineBytes(raw)
		if stats.ProjectedBytes == projectedBytes {
			break
		}
		stats.ProjectedBytes = projectedBytes
		stats.SavedBytes = stats.RawBytes - stats.ProjectedBytes
		if stats.SavedBytes < 0 {
			stats.SavedBytes = 0
		}
	}
	return nil
}

func encodedJSONLineBytes(raw []byte) int {
	return len(raw) + parentActionTerminalJSONLineBytes
}

func projectTerminalSemanticResult(raw json.RawMessage) (json.RawMessage, string, []string, error) {
	object, err := decodeJSONObject(raw, "parent action terminal")
	if err != nil {
		return nil, "", nil, err
	}
	if _, ok := object["error"]; ok {
		return raw, "error", nil, nil
	}
	status, _ := rawJSONString(object["status"])
	keep := terminalProjectionFields(status)
	if len(keep) == 0 {
		return raw, "full", nil, nil
	}
	projected, omitted, err := projectObjectFields(object, keep)
	if err != nil {
		return nil, "", nil, err
	}
	return projected, "semantic", omitted, nil
}

func terminalProjectionFields(status string) []string {
	switch status {
	case "NEEDS_SOL_DECISION":
		return []string{"status", "risk", "decision", "evidence", "options", "recommendation", "test_obligations", "targets", "artifacts"}
	case "NEEDS_SOL_REVIEW":
		return []string{"status", "risk", "summary", "requirement_coverage", "invariants", "test_evidence", "issues", "residual_risk", "sol_question", "targets", "artifacts"}
	case "PASS", "FIX_REQUIRED":
		return []string{"status", "risk", "summary", "requirement_coverage", "invariants", "test_evidence", "issues", "residual_risk", "targets", "artifacts"}
	case "IMPLEMENTED":
		return []string{"status", "risk", "summary", "requirement_coverage", "tests", "unverified", "parent_validation", "parent_validation_working_dir", "parent_validation_evidence", "targets", "artifacts"}
	default:
		return nil
	}
}

func projectRecoverableTerminalEvidence(raw json.RawMessage) (json.RawMessage, []string, bool, error) {
	object, err := decodeJSONObject(raw, "projected parent action terminal")
	if err != nil {
		return nil, nil, false, err
	}
	var artifacts []string
	if artifactJSON, ok := object["artifacts"]; ok {
		if err := json.Unmarshal(artifactJSON, &artifacts); err != nil {
			return nil, nil, false, fmt.Errorf("decode terminal artifact locators: %w", err)
		}
	}
	if len(artifacts) == 0 {
		return raw, nil, false, nil
	}

	projectedFields := make([]string, 0, 2)
	for _, field := range []string{"evidence", "test_evidence"} {
		value, ok := rawJSONString(object[field])
		if !ok || len(value) <= 160 {
			continue
		}
		replacement, err := json.Marshal(fmt.Sprintf("projected: raw %s omitted; artifact_count=%d; exact locators remain in targets/artifacts", field, len(artifacts)))
		if err != nil {
			return nil, nil, false, err
		}
		object[field] = replacement
		projectedFields = append(projectedFields, field)
	}
	if len(projectedFields) == 0 {
		return raw, nil, false, nil
	}
	projected, err := json.Marshal(object)
	if err != nil {
		return nil, nil, false, err
	}
	return projected, projectedFields, true, nil
}

func projectTerminalIdentity(raw json.RawMessage) (json.RawMessage, error) {
	object, err := decodeJSONObject(raw, "parent action terminal")
	if err != nil {
		return nil, err
	}
	if errorJSON, ok := object["error"]; ok {
		errorObject, err := decodeJSONObject(errorJSON, "parent action terminal error")
		if err != nil {
			return nil, err
		}
		errorIdentity, err := projectObjectFieldsNoOmitted(errorObject, []string{"kind", "message"})
		if err != nil {
			return nil, err
		}
		return json.Marshal(map[string]json.RawMessage{"error": errorIdentity})
	}
	return projectObjectFieldsNoOmitted(object, []string{"status", "risk", "targets", "artifacts"})
}

func projectTerminalHandoff(raw json.RawMessage, overflow bool) (json.RawMessage, []string, error) {
	object, err := decodeJSONObject(raw, "canonical handoff")
	if err != nil {
		return nil, nil, err
	}
	keep := terminalHandoffProjectionFields()
	if overflow {
		keep = []string{"version", "consistent", "inconsistency", "task_id", "task_status", "required_action", "action_specs"}
		if err := reduceHandoffToRequiredActionSpec(object); err != nil {
			return nil, nil, err
		}
	}
	return projectObjectFields(object, keep)
}

func terminalHandoffProjectionFields() []string {
	return []string{
		"version",
		"consistent",
		"inconsistency",
		"task_id",
		"task_status",
		"required_action",
		"allowed_actions",
		"required_action_parameters",
		"resume_kind",
		"pending_decision",
		"parent_review_open",
		"artifact_dir",
		"last_material",
		"session_rotation",
		"parent_request",
		"action_specs",
	}
}

func reduceHandoffToRequiredActionSpec(object map[string]json.RawMessage) error {
	requiredAction, ok := rawJSONString(object["required_action"])
	if !ok {
		return nil
	}
	specsJSON, ok := object["action_specs"]
	if !ok {
		return nil
	}
	var allSpecs map[string]json.RawMessage
	if err := json.Unmarshal(specsJSON, &allSpecs); err != nil {
		return fmt.Errorf("decode canonical handoff action_specs: %w", err)
	}
	requiredSpec, ok := allSpecs[requiredAction]
	if !ok {
		return nil
	}
	reducedSpecs, err := json.Marshal(map[string]json.RawMessage{requiredAction: requiredSpec})
	if err != nil {
		return fmt.Errorf("encode required canonical handoff action spec: %w", err)
	}
	object["action_specs"] = reducedSpecs
	return nil
}

func decodeJSONObject(raw json.RawMessage, label string) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	var object map[string]json.RawMessage
	if err := decoder.Decode(&object); err != nil {
		return nil, fmt.Errorf("%s is not a JSON object: %w", label, err)
	}
	if object == nil {
		return nil, fmt.Errorf("%s is not a JSON object", label)
	}
	return object, nil
}

func projectObjectFields(object map[string]json.RawMessage, keep []string) (json.RawMessage, []string, error) {
	keepSet := make(map[string]struct{}, len(keep))
	projected := make(map[string]json.RawMessage, len(keep))
	for _, key := range keep {
		keepSet[key] = struct{}{}
		if value, ok := object[key]; ok {
			projected[key] = value
		}
	}
	omitted := make([]string, 0, len(object))
	for key := range object {
		if _, ok := keepSet[key]; !ok {
			omitted = append(omitted, key)
		}
	}
	sort.Strings(omitted)
	raw, err := json.Marshal(projected)
	if err != nil {
		return nil, nil, err
	}
	return raw, omitted, nil
}

func projectObjectFieldsNoOmitted(object map[string]json.RawMessage, keep []string) (json.RawMessage, error) {
	raw, _, err := projectObjectFields(object, keep)
	return raw, err
}

func rawJSONString(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", false
	}
	return value, true
}

func prefixedFields(prefix string, fields []string) []string {
	result := make([]string, 0, len(fields))
	for _, field := range fields {
		result = append(result, prefix+"."+field)
	}
	return result
}

func sortedUniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	result := make([]string, 0, len(set))
	for value := range set {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
