package shadoweval

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

const referenceFixture = `{
  "schema": "system-one-shadow-reference/v1",
  "bundle_task_id": "task-1",
  "basis": "historical canonical Sol decision",
  "labels": [
    {"call_id": "call-a", "correctness_finding": true, "owner": "test-scenario", "disposition": "fix", "locator": "rollouts rows 1-10"},
    {"call_id": "call-b", "correctness_finding": false, "owner": "none", "disposition": "accept", "locator": "rollouts rows 11-20"}
  ],
  "unknown_call_ids": ["call-c", "call-d"]
}`

func shadowFixtureLogs(taskID string, base time.Time) []state.ModelCallLog {
	response, _ := json.Marshal(map[string]string{
		"summary":  "worker summary",
		"issues":   "",
		"decision": "",
	})
	return []state.ModelCallLog{
		{
			Version: state.ModelCallLogVersion, CallType: state.CallTypeEvent, CallID: "event-only",
			TaskID: taskID, Role: state.WorkerRole, Phase: "parent-decision", StartedAt: base, CompletedAt: base,
		},
		{
			Version: state.ModelCallLogVersion, CallType: state.CallTypeTask, CallID: "call-a",
			TaskID: taskID, Role: state.WorkerRole, Phase: "worker-new", PacketStatus: "IMPLEMENTED",
			Outcome: "success", StartedAt: base, CompletedAt: base.Add(time.Minute), WallDurationMS: 60000,
			Response:  string(response),
			TreeUsage: state.TokenUsage{InputTokens: 100, CacheReadInputTokens: 5, OutputTokens: 7},
		},
		{
			Version: state.ModelCallLogVersion, CallType: state.CallTypeTask, CallID: "call-b",
			TaskID: taskID, Role: state.ReviewerRole, Phase: "reviewer-1-high-floor", PacketStatus: "NEEDS_SOL_DECISION",
			Outcome: "success", StartedAt: base.Add(time.Hour), CompletedAt: base.Add(time.Hour).Add(time.Minute),
			WallDurationMS: 30000,
		},
	}
}

func shadowFixtureEvents(callID string) []state.TaskEventRecord {
	return []state.TaskEventRecord{
		{Version: 1, TaskID: "task-1", CallID: callID, Role: "worker", Phase: "worker-new", Seq: 1,
			Kind: "assistant", Blocks: []state.TaskBlockSummary{{Type: "tool_use", Name: "Read"}, {Type: "tool_use", Name: "Bash"}, {Type: "tool_use", Name: "Read"}}},
	}
}

func TestBuildInputProjectsTaskCallsWithToolsAndPacketText(t *testing.T) {
	base := time.Date(2026, 9, 24, 1, 0, 0, 0, time.UTC)
	input, err := BuildInput("task-1", shadowFixtureLogs("task-1", base), shadowFixtureEvents("call-a"))
	if err != nil {
		t.Fatal(err)
	}
	if input.Schema != InputSchema || input.TaskID != "task-1" {
		t.Fatalf("input identity = %#v", input)
	}
	if input.SourceFiles["telemetry"] != "telemetry/task-1.jsonl" || input.SourceFiles["events"] != "events/task-1.jsonl" {
		t.Fatalf("source files = %#v", input.SourceFiles)
	}
	if len(input.Items) != 2 {
		t.Fatalf("items = %#v", input.Items)
	}
	first := input.Items[0]
	if first.CallID != "call-a" || first.Role != "worker" || first.Phase != "worker-new" || first.PacketStatus != "IMPLEMENTED" {
		t.Fatalf("first item = %#v", first)
	}
	if first.Summary != "worker summary" || first.Issues != "" || first.Decision != "" {
		t.Fatalf("packet text = %#v", first)
	}
	if first.Usage.InputTokens != 100 || first.Usage.CacheReadInputTokens != 5 || first.Usage.OutputTokens != 7 {
		t.Fatalf("usage = %#v", first.Usage)
	}
	if first.ToolUseCounts["Read"] != 2 || first.ToolUseCounts["Bash"] != 1 {
		t.Fatalf("tool counts = %#v", first.ToolUseCounts)
	}
	if len(first.ToolUseCounts) != 2 {
		t.Fatalf("tool counts = %#v", first.ToolUseCounts)
	}
	if first.StartedAt != "2026-09-24T01:00:00Z" {
		t.Fatalf("started_at = %q", first.StartedAt)
	}
	if input.Items[1].ToolUseCounts != nil {
		t.Fatalf("tool counts without events = %#v", input.Items[1].ToolUseCounts)
	}
	if input.ItemsSHA256 == "" {
		t.Fatal("items_sha256がありません")
	}
	again, err := BuildInput("task-1", shadowFixtureLogs("task-1", base), shadowFixtureEvents("call-a"))
	if err != nil {
		t.Fatal(err)
	}
	if again.ItemsSHA256 != input.ItemsSHA256 {
		t.Fatalf("同一入力のshaが変動しました: %s != %s", again.ItemsSHA256, input.ItemsSHA256)
	}
}

func TestBuildInputBoundsPacketTextToValidUTF8(t *testing.T) {
	long := strings.Repeat("あ", 300) + "tail-end"
	response, _ := json.Marshal(map[string]string{"summary": long})
	logs := []state.ModelCallLog{{
		Version: state.ModelCallLogVersion, CallType: state.CallTypeTask, CallID: "call-a",
		TaskID: "task-1", Role: state.WorkerRole, Phase: "worker-new", Response: string(response),
	}}
	input, err := BuildInput("task-1", logs, nil)
	if err != nil {
		t.Fatal(err)
	}
	summary := input.Items[0].Summary
	if len(summary) > packetTextBoundBytes {
		t.Fatalf("summary長 = %d", len(summary))
	}
	if !strings.HasPrefix(summary, packetTextOmissionMarker) || !strings.HasSuffix(summary, "tail-end") {
		t.Fatalf("summary末尾保持とmarkerが崩れています: %q", summary[:64])
	}
	if !utf8.ValidString(summary) {
		t.Fatal("summaryが不正UTF-8です")
	}
}

func TestBuildInputRejectsInputWithoutTaskCalls(t *testing.T) {
	logs := []state.ModelCallLog{{
		Version: state.ModelCallLogVersion, CallType: state.CallTypeEvent, CallID: "event-only",
		TaskID: "task-1", Role: state.WorkerRole, Phase: "parent-decision",
	}}
	if _, err := BuildInput("task-1", logs, nil); err == nil || !strings.Contains(err.Error(), "task呼出telemetry") {
		t.Fatalf("task呼用なしはerror: %v", err)
	}
}

func validDecision(callID string, noise float64, risk float64, disposition string, owner string) Decision {
	return Decision{
		CallID:                     callID,
		NoiseProbability:           noise,
		NoiseConfidence:            0.8,
		LegitimateRetryProbability: 0.1,
		LegitimateRetryConfidence:  0.7,
		CorrectnessRiskProbability: risk,
		CorrectnessRiskConfidence:  0.7,
		OwnerCategory:              owner,
		OwnerProbability:           0.95,
		OwnerConfidence:            0.9,
		DispositionCategory:        disposition,
		DispositionProbability:     0.8,
		DispositionConfidence:      0.7,
		SolEscalationProbability:   0.1,
		SolEscalationConfidence:    0.6,
	}
}

func twoItemInput() ShadowInput {
	return ShadowInput{
		Schema: InputSchema, TaskID: "task-1", Items: []InputItem{
			{CallID: "call-a", Role: "worker", Phase: "worker-new"},
			{CallID: "call-b", Role: "reviewer", Phase: "reviewer-1"},
		},
	}
}

func decisionsRaw(decisions ...any) json.RawMessage {
	data, err := json.Marshal(map[string]any{"decisions": decisions})
	if err != nil {
		panic(err)
	}
	return data
}

func TestParseDecisionsAcceptsFullCoverage(t *testing.T) {
	raw := decisionsRaw(
		validDecision("call-a", 0.05, 0.2, "accept", "worker"),
		validDecision("call-b", 0.05, 0.9, "needs-investigation", "reviewer"),
	)
	valid, problems := ParseDecisions(raw, twoItemInput())
	if len(problems) != 0 || len(valid) != 2 {
		t.Fatalf("problems=%v valid=%d", problems, len(valid))
	}
}

func TestParseDecisionsRecordsSchemaViolationsWithoutDroppingOtherItems(t *testing.T) {
	withUnknown := map[string]any{}
	rawBytes, _ := json.Marshal(validDecision("call-a", 0.05, 0.2, "accept", "worker"))
	if err := json.Unmarshal(rawBytes, &withUnknown); err != nil {
		t.Fatal(err)
	}
	withUnknown["free_text"] = "schema外"
	raw := decisionsRaw(withUnknown, validDecision("call-b", 0.05, 0.9, "fix", "reviewer"))

	valid, problems := ParseDecisions(raw, twoItemInput())
	if len(valid) != 1 || valid[0].CallID != "call-b" {
		t.Fatalf("valid = %#v", valid)
	}
	if len(problems) != 2 {
		t.Fatalf("problems = %#v", problems)
	}
	if !strings.Contains(problems[0], "decision構造が不正") {
		t.Fatalf("schema外問題 = %q", problems[0])
	}
	if !strings.Contains(problems[1], "call-a: decisionが欠落") {
		t.Fatalf("欠落問題 = %q", problems[1])
	}
}

func TestParseDecisionsRejectsMissingAndNullRequiredFieldsButKeepsExplicitZero(t *testing.T) {
	input := ShadowInput{Schema: InputSchema, TaskID: "task-1", Items: []InputItem{{CallID: "call-a"}, {CallID: "call-b"}, {CallID: "call-c"}}}
	base, err := json.Marshal(validDecision("call-a", 0.05, 0.2, "accept", "worker"))
	if err != nil {
		t.Fatal(err)
	}
	missingFields := map[string]any{}
	if err := json.Unmarshal(base, &missingFields); err != nil {
		t.Fatal(err)
	}
	delete(missingFields, "noise_confidence")
	delete(missingFields, "correctness_risk_probability")

	nullFields := map[string]any{}
	if err := json.Unmarshal(base, &nullFields); err != nil {
		t.Fatal(err)
	}
	nullFields["noise_confidence"] = nil
	nullFields["correctness_risk_probability"] = nil

	zeroBase, err := json.Marshal(validDecision("call-b", 0.05, 0, "accept", "worker"))
	if err != nil {
		t.Fatal(err)
	}
	explicitZero := map[string]any{}
	if err := json.Unmarshal(zeroBase, &explicitZero); err != nil {
		t.Fatal(err)
	}
	explicitZero["noise_confidence"] = 0

	valid, problems := ParseDecisions(decisionsRaw(missingFields, explicitZero, nullFields), input)
	if len(problems) != 4 {
		t.Fatalf("problems = %#v", problems)
	}
	if !strings.Contains(problems[0], "noise_confidence") || !strings.Contains(problems[0], "correctness_risk_probability") {
		t.Fatalf("欠落field問題 = %q", problems[0])
	}
	if !strings.Contains(problems[1], "noise_confidence") || !strings.Contains(problems[1], "correctness_risk_probability") {
		t.Fatalf("null field問題 = %q", problems[1])
	}
	if !strings.Contains(problems[2], "call-a: decisionが欠落しています") || !strings.Contains(problems[3], "call-c: decisionが欠落しています") {
		t.Fatalf("欠落decision問題 = %q, %q", problems[2], problems[3])
	}
	if len(valid) != 1 || valid[0].CallID != "call-b" || valid[0].NoiseConfidence != 0 || valid[0].CorrectnessRiskProbability != 0 {
		t.Fatalf("明示0のdecision = %#v", valid)
	}
}

func TestParseDecisionsRecordsRangeEnumDuplicateAndUnknownCall(t *testing.T) {
	input := ShadowInput{Schema: InputSchema, TaskID: "task-1", Items: []InputItem{{CallID: "call-a"}, {CallID: "call-b"}, {CallID: "call-c"}}}
	outOfRange := validDecision("call-a", 1.5, 0.2, "accept", "worker")
	badEnum := validDecision("call-b", 0.05, 0.2, "wrong-disposition", "worker")
	unknownCall := validDecision("call-z", 0.05, 0.2, "accept", "worker")
	replacement := validDecision("call-a", 0.05, 0.2, "accept", "worker")

	valid, problems := ParseDecisions(decisionsRaw(outOfRange, badEnum, unknownCall, replacement), input)
	if len(valid) != 1 || valid[0].CallID != "call-a" {
		t.Fatalf("valid = %#v", valid)
	}
	if len(problems) != 5 {
		t.Fatalf("problems = %#v", problems)
	}
	if !strings.Contains(problems[0], "noise_probabilityが0..1の範囲外") {
		t.Fatalf("range問題 = %q", problems[0])
	}
	if !strings.Contains(problems[1], "disposition_categoryが不正") {
		t.Fatalf("enum問題 = %q", problems[1])
	}
	if !strings.Contains(problems[2], "入力に存在しないcall_id") {
		t.Fatalf("unknown call問題 = %q", problems[2])
	}
	if !strings.Contains(problems[3], "call-b: decisionが欠落しています") || !strings.Contains(problems[4], "call-c: decisionが欠落しています") {
		t.Fatalf("欠落問題 = %#v", problems[3:])
	}

	duplicate := validDecision("call-a", 0.05, 0.2, "accept", "worker")
	valid, problems = ParseDecisions(decisionsRaw(replacement, duplicate), input)
	if len(valid) != 1 {
		t.Fatalf("valid = %#v", valid)
	}
	if len(problems) != 3 || !strings.Contains(problems[0], "call-a: call_idが重複") || !strings.Contains(problems[1], "call-b: decisionが欠落") || !strings.Contains(problems[2], "call-c: decisionが欠落") {
		t.Fatalf("dup problems = %#v", problems)
	}
}

func TestParseDecisionsRejectsMissingOutputAndBrokenEnvelope(t *testing.T) {
	if _, problems := ParseDecisions(nil, twoItemInput()); len(problems) != 1 || problems[0] != "decision出力がありません" {
		t.Fatalf("problems = %#v", problems)
	}
	if _, problems := ParseDecisions(json.RawMessage(`not-json`), twoItemInput()); len(problems) != 1 || !strings.Contains(problems[0], "decision出力の構造が不正") {
		t.Fatalf("problems = %#v", problems)
	}
	if _, problems := ParseDecisions(json.RawMessage(`{"decisions":[],"extra":1}`), twoItemInput()); len(problems) != 1 || !strings.Contains(problems[0], "decision出力の構造が不正") {
		t.Fatalf("envelope schema外 = %#v", problems)
	}
}

func TestDecisionsJSONSchemaPinsItemCountAndIsDeterministic(t *testing.T) {
	first := DecisionsJSONSchema(10)
	second := DecisionsJSONSchema(10)
	if first != second {
		t.Fatal("schemaが決定論的ではありません")
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(first), &decoded); err != nil {
		t.Fatal(err)
	}
	decisions := decoded["properties"].(map[string]any)["decisions"].(map[string]any)
	if decisions["minItems"] != float64(10) || decisions["maxItems"] != float64(10) {
		t.Fatalf("min/max = %v/%v", decisions["minItems"], decisions["maxItems"])
	}
	item := decisions["items"].(map[string]any)
	if len(item["required"].([]any)) != 15 {
		t.Fatalf("required = %v", item["required"])
	}
	if item["additionalProperties"] != false {
		t.Fatal("additionalPropertiesはfalseであるべきです")
	}
}

func writeReferenceFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "reference.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadReferenceAcceptsCurrentSchema(t *testing.T) {
	reference, err := LoadReference(writeReferenceFile(t, referenceFixture))
	if err != nil {
		t.Fatal(err)
	}
	if reference.Schema != ReferenceSchema || reference.TaskID != "task-1" || len(reference.Labels) != 2 || len(reference.UnknownCallIDs) != 2 {
		t.Fatalf("reference = %#v", reference)
	}
}

func TestLoadReferenceRejectsLegacySchemaAndMalformedLabels(t *testing.T) {
	legacy := strings.Replace(referenceFixture, ReferenceSchema, "system-one-shadow-poc-reference/v1", 1)
	if _, err := LoadReference(writeReferenceFile(t, legacy)); err == nil || !strings.Contains(err.Error(), "reference schemaが不正") {
		t.Fatalf("旧schemaはreject: %v", err)
	}
	dup := strings.Replace(referenceFixture, `"call-c"`, `"call-a"`, 1)
	if _, err := LoadReference(writeReferenceFile(t, dup)); err == nil || !strings.Contains(err.Error(), "重複") {
		t.Fatalf("重複はreject: %v", err)
	}
	badDisposition := strings.Replace(referenceFixture, `"disposition": "accept"`, `"disposition": "ship-it"`, 1)
	if _, err := LoadReference(writeReferenceFile(t, badDisposition)); err == nil || !strings.Contains(err.Error(), "dispositionが不正") {
		t.Fatalf("enum違反はreject: %v", err)
	}
	unknownField := strings.Replace(referenceFixture, `"basis": "historical canonical Sol decision",`, `"basis": "historical canonical Sol decision", "memo": "x",`, 1)
	if _, err := LoadReference(writeReferenceFile(t, unknownField)); err == nil || !strings.Contains(err.Error(), "解析できません") {
		t.Fatalf("schema外fieldはreject: %v", err)
	}
	if _, err := LoadReference(filepath.Join(t.TempDir(), "missing.json")); err == nil || !strings.Contains(err.Error(), "reference fileを読めません") {
		t.Fatalf("file欠落はreject: %v", err)
	}
}

func TestValidateReferenceAgainstInputBindsTaskAndCallIDs(t *testing.T) {
	reference, err := LoadReference(writeReferenceFile(t, referenceFixture))
	if err != nil {
		t.Fatal(err)
	}
	input := ShadowInput{Schema: InputSchema, TaskID: "task-1", Items: []InputItem{
		{CallID: "call-a"}, {CallID: "call-b"}, {CallID: "call-c"}, {CallID: "call-d"},
	}}
	if err := ValidateReferenceAgainstInput(reference, input); err != nil {
		t.Fatalf("一致する組み合わせは通過すべき: %v", err)
	}
	wrongTask := input
	wrongTask.TaskID = "task-2"
	if err := ValidateReferenceAgainstInput(reference, wrongTask); err == nil || !strings.Contains(err.Error(), "bundle_task_idが対象taskと一致しません") {
		t.Fatalf("task不一致はreject: %v", err)
	}
	shortInput := ShadowInput{Schema: InputSchema, TaskID: "task-1", Items: []InputItem{{CallID: "call-a"}}}
	if err := ValidateReferenceAgainstInput(reference, shortInput); err == nil || !strings.Contains(err.Error(), "入力itemsに存在しません") {
		t.Fatalf("label不一致はreject: %v", err)
	}
}

func comparisonFixtureInput() ShadowInput {
	return ShadowInput{
		Schema: InputSchema, TaskID: "task-1",
		Items: []InputItem{
			{CallID: "call-a", Role: "worker", Phase: "worker-new", PacketStatus: "IMPLEMENTED", Summary: strings.Repeat("a", 400)},
			{CallID: "call-b", Role: "reviewer", Phase: "reviewer-1", PacketStatus: "FIX_REQUIRED", Summary: "b"},
			{CallID: "call-c", Role: "worker", Phase: "worker-fix", PacketStatus: "IMPLEMENTED", Summary: strings.Repeat("c", 100)},
			{CallID: "call-d", Role: "reviewer", Phase: "reviewer-2", PacketStatus: "NEEDS_SOL_REVIEW", Summary: "d"},
		},
	}
}

func comparisonFixtureDecisions() []Decision {
	return []Decision{
		validDecision("call-a", 0.05, 0.2, "accept", "worker"),
		validDecision("call-b", 0.05, 0.6, "fix", "worker"),
		validDecision("call-c", 0.05, 0.1, "accept", "worker"),
		validDecision("call-d", 0.05, 0.1, "accept", "reviewer"),
	}
}

func comparisonFixtureReference() Reference {
	return Reference{
		Schema: ReferenceSchema, TaskID: "task-1", Basis: "historical canonical Sol decision",
		Labels: []ReferenceLabel{
			{CallID: "call-b", CorrectnessFinding: true, Owner: "test-scenario", Disposition: "fix", Locator: "rows"},
			{CallID: "call-d", CorrectnessFinding: false, Owner: "none", Disposition: "accept", Locator: "rows"},
		},
	}
}

func TestBuildComparisonMeasuresAgreementBrierCoverageAndRunFlags(t *testing.T) {
	input := comparisonFixtureInput()
	run := NewRun(CallMetrics{DurationMS: 16551, TotalCostUSD: 0.077, InputBytes: 15000, Usage: UsageMetrics{InputTokens: 5688, OutputTokens: 1970}}, []string{"ANTHROPIC_AUTH_TOKEN"})
	comparison := BuildComparison(input, comparisonFixtureDecisions(), nil, nil, comparisonFixtureReference(), run)

	if comparison.Schema != ComparisonSchema || comparison.InputItems != 4 {
		t.Fatalf("comparison = %#v", comparison)
	}
	if !comparison.TypedSchemaValid {
		t.Fatalf("typed_schema_valid = %v errors=%v", comparison.TypedSchemaValid, comparison.ValidationErrors)
	}
	if comparison.ReferenceKnown != 2 || comparison.ReferenceUnknown != 2 {
		t.Fatalf("reference known/unknown = %d/%d", comparison.ReferenceKnown, comparison.ReferenceUnknown)
	}
	if comparison.DispositionAgreementKnown.Matching != 2 || comparison.DispositionAgreementKnown.Denominator != 2 {
		t.Fatalf("agreement = %#v", comparison.DispositionAgreementKnown)
	}
	if comparison.CorrectnessRiskBrierKnown == nil || math.Abs(*comparison.CorrectnessRiskBrierKnown-0.085) > 0.0001 {
		brier := -1.0
		if comparison.CorrectnessRiskBrierKnown != nil {
			brier = *comparison.CorrectnessRiskBrierKnown
		}
		t.Fatalf("brier = %v", brier)
	}
	if len(comparison.Thresholds) != 4 {
		t.Fatalf("thresholds = %#v", comparison.Thresholds)
	}
	for _, row := range comparison.Thresholds {
		if row.Candidates != 0 || row.Coverage != 0 || row.KnownCorrectnessFalseNegatives != 0 || row.UnknownReferenceCandidates != 0 {
			t.Fatalf("noise候補0のthreshold行 = %#v", row)
		}
	}
	if len(comparison.ReductionGroups) != 0 {
		t.Fatalf("reduction groups = %#v", comparison.ReductionGroups)
	}
	if len(comparison.DecisionsWithReference) != 4 {
		t.Fatalf("rows = %#v", comparison.DecisionsWithReference)
	}
	if comparison.DecisionsWithReference[1].Reference == nil || comparison.DecisionsWithReference[1].Reference.Disposition != "fix" {
		t.Fatalf("reference row = %#v", comparison.DecisionsWithReference[1])
	}
	if comparison.DecisionsWithReference[0].Reference != nil {
		t.Fatalf("unknown row = %#v", comparison.DecisionsWithReference[0])
	}
	if !comparison.Run.SettingsSourceDisabled || !comparison.Run.NoSessionPersistence || !comparison.Run.ToolsDisabled {
		t.Fatalf("run隔離flags = %#v", comparison.Run)
	}
	if comparison.Run.UserSettingsLoadedByCLI || comparison.Run.ControlAuthority || comparison.Run.FilterAuthority || comparison.Run.AuditMutated {
		t.Fatalf("authority flags = %#v", comparison.Run)
	}
	if comparison.Run.Provider != "claude-cli-isolated-one-shot" || comparison.Run.ModelAlias != "opus" || comparison.Run.ModelEffort != "low" {
		t.Fatalf("run identity = %#v", comparison.Run)
	}
}

func TestThresholdCandidatesRequireNoiseDispositionConfidenceAndLowRisk(t *testing.T) {
	input := ShadowInput{Schema: InputSchema, TaskID: "task-1", Items: []InputItem{
		{CallID: "noise-ok"}, {CallID: "accept-verdict"}, {CallID: "low-conf"}, {CallID: "high-risk"}, {CallID: "high-escalation"},
	}}
	decisions := []Decision{
		candidateReadyDecision("noise-ok", "noise", 0.95, 0.95, 0.05, 0.05),
		candidateReadyDecision("accept-verdict", "accept", 0.95, 0.95, 0.05, 0.05),
		candidateReadyDecision("low-conf", "noise", 0.95, 0.3, 0.05, 0.05),
		candidateReadyDecision("high-risk", "noise", 0.95, 0.95, 0.5, 0.05),
		candidateReadyDecision("high-escalation", "noise", 0.95, 0.95, 0.05, 0.5),
	}
	comparison := BuildComparison(input, decisions, nil, nil, Reference{}, NewRun(CallMetrics{}, nil))

	for _, row := range comparison.Thresholds {
		if row.Candidates != 1 || len(row.CandidateCallIDs) != 1 || row.CandidateCallIDs[0] != "noise-ok" {
			t.Fatalf("threshold %v candidates = %#v", row.Threshold, row)
		}
		if row.Coverage != 0.2 {
			t.Fatalf("threshold %v coverage = %v", row.Threshold, row.Coverage)
		}
	}
	if len(comparison.ReductionGroups) != 1 || comparison.ReductionGroups[0].DispositionCategory != "noise" {
		t.Fatalf("reduction groups = %#v", comparison.ReductionGroups)
	}
	if len(comparison.ReductionGroups[0].CallIDs) != 1 || comparison.ReductionGroups[0].CallIDs[0] != "noise-ok" {
		t.Fatalf("group members = %#v", comparison.ReductionGroups[0])
	}
}

func candidateReadyDecision(callID string, disposition string, noise, confidence, risk, escalation float64) Decision {
	decision := validDecision(callID, noise, risk, disposition, "worker")
	decision.NoiseConfidence = confidence
	decision.SolEscalationProbability = escalation
	return decision
}

func TestBuildComparisonSchemaFailureKeepsFailureAndZeroesReductionClaims(t *testing.T) {
	input := comparisonFixtureInput()
	decisions := comparisonFixtureDecisions()
	decisions[2].DispositionCategory = "noise"
	decisions[2].NoiseProbability = 0.95
	decisions[2].NoiseConfidence = 0.95
	run := NewRun(CallMetrics{}, nil)
	failure := &ShadowFailure{Kind: ShadowFailureSchemaInvalid}
	comparison := BuildComparison(input, decisions, []string{"call-z: noise_probabilityが0..1の範囲外です(1.5)"}, failure, comparisonFixtureReference(), run)

	if comparison.TypedSchemaValid || comparison.ShadowFailure != failure || len(comparison.ValidationErrors) != 1 {
		t.Fatalf("schema invalid観測 = %#v", comparison)
	}
	if len(comparison.Thresholds) != 4 {
		t.Fatalf("thresholds = %#v", comparison.Thresholds)
	}
	for _, row := range comparison.Thresholds {
		if row.Candidates != 0 || len(row.CandidateCallIDs) != 0 || row.Coverage != 0 || row.EstimatedSolVisibleInputReduction != 0 {
			t.Fatalf("schema失敗時の削減主張 = %#v", row)
		}
		if row.KnownCorrectnessFalseNegatives != 0 || row.UnknownReferenceCandidates != 0 {
			t.Fatalf("schema失敗時の候補tally = %#v", row)
		}
	}
	if len(comparison.ReductionGroups) != 0 {
		t.Fatalf("reduction groups = %#v", comparison.ReductionGroups)
	}
	if len(comparison.DecisionsWithReference) != 4 {
		t.Fatalf("対応付け行 = %#v", comparison.DecisionsWithReference)
	}
}

func TestBuildComparisonCountsKnownCorrectnessFalseNegatives(t *testing.T) {
	input := comparisonFixtureInput()
	decisions := comparisonFixtureDecisions()
	decisions[1].DispositionCategory = "noise"
	decisions[1].NoiseProbability = 0.8
	decisions[1].CorrectnessRiskProbability = 0.05
	comparison := BuildComparison(input, decisions, nil, nil, comparisonFixtureReference(), NewRun(CallMetrics{}, nil))
	for _, row := range comparison.Thresholds {
		expected := 0
		if row.Threshold <= 0.8 {
			expected = 1
		}
		if row.KnownCorrectnessFalseNegatives != expected {
			t.Fatalf("threshold %v FN = %d (want %d)", row.Threshold, row.KnownCorrectnessFalseNegatives, expected)
		}
	}
}

func TestBuildComparisonWithoutReferenceOrDecisionsKeepsUnknownHonest(t *testing.T) {
	input := comparisonFixtureInput()
	failure := &ShadowFailure{Kind: ShadowFailureProvider, Detail: "probe失敗"}
	comparison := BuildComparison(input, nil, []string{"call-a: decisionが欠落しています", "call-b: decisionが欠落しています", "call-c: decisionが欠落しています", "call-d: decisionが欠落しています"}, failure, Reference{}, NewRun(CallMetrics{}, nil))
	if comparison.ReferenceKnown != 0 || comparison.ReferenceUnknown != 4 {
		t.Fatalf("unknown = %d/%d", comparison.ReferenceKnown, comparison.ReferenceUnknown)
	}
	if comparison.CorrectnessRiskBrierKnown != nil {
		t.Fatalf("brier = %v", comparison.CorrectnessRiskBrierKnown)
	}
	if comparison.DispositionAgreementKnown.Denominator != 0 {
		t.Fatalf("agreement = %#v", comparison.DispositionAgreementKnown)
	}
	if comparison.TypedSchemaValid || comparison.ShadowFailure.Kind != ShadowFailureProvider {
		t.Fatalf("provider失敗観測 = %#v", comparison.ShadowFailure)
	}
	if len(comparison.Thresholds) != 4 {
		t.Fatalf("thresholds = %#v", comparison.Thresholds)
	}
	for _, row := range comparison.Thresholds {
		if row.Candidates != 0 || row.Coverage != 0 || row.EstimatedSolVisibleInputReduction != 0 {
			t.Fatalf("provider失敗時の削減主張 = %#v", row)
		}
	}
	if len(comparison.ReductionGroups) != 0 {
		t.Fatalf("provider失敗時のreduction groups = %#v", comparison.ReductionGroups)
	}
}

func TestClassificationPromptEmbedsExactInputJSON(t *testing.T) {
	input := comparisonFixtureInput()
	marshaled, err := MarshalInput(input)
	if err != nil {
		t.Fatal(err)
	}
	prompt := ClassificationPrompt(input, marshaled)
	if !strings.HasSuffix(prompt, string(marshaled)) {
		t.Fatal("promptが直列化input JSONとbyte一致するsnapshotを含んでいません")
	}
	if prompt == string(marshaled) {
		t.Fatal("promptに入力JSON以外の指示がありません")
	}
}
