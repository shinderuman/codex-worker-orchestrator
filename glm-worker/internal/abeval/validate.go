package abeval

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func LoadPair(dir string) (Spec, RunRecord, RunRecord, error) {
	spec, err := LoadSpec(filepath.Join(dir, "spec.json"))
	if err != nil {
		return Spec{}, RunRecord{}, RunRecord{}, err
	}
	direct, err := LoadRecord(filepath.Join(dir, "direct.json"))
	if err != nil {
		return Spec{}, RunRecord{}, RunRecord{}, fmt.Errorf("direct記録の読み込み: %w", err)
	}
	orchestrated, err := LoadRecord(filepath.Join(dir, "orchestrated.json"))
	if err != nil {
		return Spec{}, RunRecord{}, RunRecord{}, fmt.Errorf("orchestrated記録の読み込み: %w", err)
	}
	return spec, direct, orchestrated, nil
}

func LoadSpec(path string) (Spec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Spec{}, fmt.Errorf("specを読めません: %w", err)
	}
	var spec Spec
	if err := decodeStrict(data, "spec", &spec); err != nil {
		return Spec{}, err
	}
	if spec.Version != specVersion {
		return Spec{}, fmt.Errorf("spec versionは%dである必要があります: %d", specVersion, spec.Version)
	}
	return spec, nil
}

func LoadRecord(path string) (RunRecord, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return RunRecord{}, fmt.Errorf("run記録を読めません: %w", err)
	}
	var record RunRecord
	if err := decodeStrict(data, "run記録", &record); err != nil {
		return RunRecord{}, err
	}
	if record.Version != runRecordVersion {
		return RunRecord{}, fmt.Errorf("run記録versionは%dである必要があります: %d", runRecordVersion, record.Version)
	}
	if record.Mode != ModeDirect && record.Mode != ModeOrchestrated {
		return RunRecord{}, fmt.Errorf("modeは%sか%sである必要があります: %s", ModeDirect, ModeOrchestrated, record.Mode)
	}
	return record, nil
}

func decodeStrict(data []byte, what string, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("%sを読めません: %w", what, err)
	}
	var extra json.RawMessage
	if err := dec.Decode(&extra); err != io.EOF {
		return fmt.Errorf("%sには複数のJSON値が含まれています: %w", what, err)
	}
	return nil
}

func ValidateSpec(spec Spec) error {
	if spec.Version != specVersion {
		return fmt.Errorf("spec versionは%dである必要があります: %d", specVersion, spec.Version)
	}
	if err := validateSpecRequiredFields(spec); err != nil {
		return err
	}
	if !isGitObjectHash(spec.RepoSnapshotCommit) {
		return fmt.Errorf("repo_snapshot_commitがgit object hash形式ではありません: %s", spec.RepoSnapshotCommit)
	}
	if spec.MeasurementBoundary != CanonicalMeasurementBoundary {
		return fmt.Errorf("measurement_boundaryは計測境界契約と一致する必要があります: %q", spec.MeasurementBoundary)
	}
	return validateSpecIsolation(spec)
}

func validateSpecRequiredFields(spec Spec) error {
	fields := []struct {
		name  string
		value string
	}{
		{name: "id", value: spec.ID},
		{name: "user_request", value: spec.UserRequest},
		{name: "repo_snapshot_commit", value: spec.RepoSnapshotCommit},
		{name: "initial_worktree", value: spec.InitialWorktree},
		{name: "completion_conditions", value: spec.CompletionConditions},
		{name: "quality_verification", value: spec.QualityVerification},
		{name: "codex_model", value: spec.CodexModel},
		{name: "codex_reasoning_effort", value: spec.CodexReasoningEffort},
	}
	empty := make([]string, 0, len(fields))
	for _, field := range fields {
		if field.value == "" {
			empty = append(empty, field.name)
		}
	}
	if len(empty) > 0 {
		return fmt.Errorf("specの必須fieldが空です: %s", strings.Join(empty, ","))
	}
	return nil
}

func validateSpecIsolation(spec Spec) error {
	if !spec.Isolation.IndependentSession || !spec.Isolation.IndependentWorktree {
		return fmt.Errorf("isolationは独立session・独立working treeを比較前提として要求します: %+v", spec.Isolation)
	}
	if spec.Isolation.CacheAvoidance == "" {
		return fmt.Errorf("isolation.cache_avoidanceが空です。先行runやcacheによる比較汚染回避手段を宣言してください")
	}
	return nil
}

func isGitObjectHash(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	for _, c := range value {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func ValidatePair(spec Spec, direct, orchestrated RunRecord) error {
	if err := ValidateSpec(spec); err != nil {
		return err
	}
	if direct.Mode != ModeDirect {
		return fmt.Errorf("1つ目の記録はmode %sである必要があります: %s", ModeDirect, direct.Mode)
	}
	if orchestrated.Mode != ModeOrchestrated {
		return fmt.Errorf("2つ目の記録はmode %sである必要があります: %s", ModeOrchestrated, orchestrated.Mode)
	}
	if err := validateRecord(spec, direct); err != nil {
		return fmt.Errorf("direct記録: %w", err)
	}
	if err := validateRecord(spec, orchestrated); err != nil {
		return fmt.Errorf("orchestrated記録: %w", err)
	}
	if direct.SessionID == orchestrated.SessionID {
		return fmt.Errorf("両modeが同一session %qを使っています。独立sessionで比較してください", direct.SessionID)
	}
	if filepath.Clean(direct.WorktreePath) == filepath.Clean(orchestrated.WorktreePath) {
		return fmt.Errorf("両modeが同一working tree %qを使っています。独立working treeで比較してください", direct.WorktreePath)
	}
	return nil
}

func validateRecord(spec Spec, record RunRecord) error {
	if err := validateRecordIdentity(spec, record); err != nil {
		return err
	}
	if err := validateRunBoundary(record); err != nil {
		return err
	}
	if err := validateCodexUsage(record.CodexUsage); err != nil {
		return err
	}
	if err := validateQuality(record.Quality); err != nil {
		return err
	}
	if err := validateGLMUsage(record); err != nil {
		return err
	}
	return validateProxy(record.Proxy)
}

func validateRecordIdentity(spec Spec, record RunRecord) error {
	if record.SpecID != spec.ID {
		return fmt.Errorf("spec_id %qはspec %qと一致しません", record.SpecID, spec.ID)
	}
	if record.SpecSHA256 != SpecSHA256(spec) {
		return fmt.Errorf("spec_sha256が現在のspecと一致しません。recordは別revisionのspecで作成されています")
	}
	if record.SessionID == "" {
		return fmt.Errorf("session_idが空です")
	}
	if !filepath.IsAbs(record.WorktreePath) {
		return fmt.Errorf("worktree_pathは絶対pathである必要があります: %s", record.WorktreePath)
	}
	expected := RunConditions{
		RepoSnapshotCommit:   spec.RepoSnapshotCommit,
		InitialWorktree:      spec.InitialWorktree,
		CodexModel:           spec.CodexModel,
		CodexReasoningEffort: spec.CodexReasoningEffort,
	}
	if record.RunConditions != expected {
		return fmt.Errorf("run_conditionsがspecの固定条件と一致しません: %+v want %+v", record.RunConditions, expected)
	}
	return nil
}

func validateRunBoundary(record RunRecord) error {
	if record.Boundary.StartedAt.IsZero() || record.Boundary.CompletedAt.IsZero() {
		return fmt.Errorf("boundaryの時刻が未設定です")
	}
	if !record.Boundary.StartedAt.Before(record.Boundary.CompletedAt) {
		return fmt.Errorf("boundaryの開始時刻が完了時刻以降です: %s >= %s", record.Boundary.StartedAt, record.Boundary.CompletedAt)
	}
	return nil
}

func validateCodexUsage(usage CodexUsage) error {
	if usage.Source != "" && usage.Source != CodexUsageSourceAppExport {
		return fmt.Errorf("codex_usage.sourceは%sのみ受理します。取得できない場合はunknown(source空)としてください: %q", CodexUsageSourceAppExport, usage.Source)
	}
	if usage.InputTokens < 0 || usage.OutputTokens < 0 {
		return fmt.Errorf("codex_usageのtoken値が負です: %+v", usage)
	}
	if !usage.Known() && (usage.InputTokens != 0 || usage.OutputTokens != 0) {
		return fmt.Errorf("codex_usageはsourceなしにtoken値を持てません。actual usageを取得できない場合はunknown(零値)としてください")
	}
	return nil
}

func validateQuality(quality Quality) error {
	switch quality.HiddenVerification {
	case "pass", "fail", "not-run":
	default:
		return fmt.Errorf("quality.hidden_verificationはpass/fail/not-runである必要があります: %q", quality.HiddenVerification)
	}
	if quality.TestsRun < 0 || quality.TestFailures < 0 {
		return fmt.Errorf("qualityのtest数が負です: %+v", quality)
	}
	if quality.TestFailures > quality.TestsRun {
		return fmt.Errorf("quality.test_failuresがtests_runを超えています: %+v", quality)
	}
	if quality.EscapedBugs < 0 || quality.ScopeViolations < 0 {
		return fmt.Errorf("qualityのescaped_bugs/scope_violationsが負です: %+v", quality)
	}
	return nil
}

func validateGLMUsage(record RunRecord) error {
	usage := record.GLMUsage
	if usage.InputTokens < 0 || usage.CacheCreationInputTokens < 0 ||
		usage.CacheReadInputTokens < 0 || usage.OutputTokens < 0 || usage.ModelCalls < 0 {
		return fmt.Errorf("glm_usageの値が負です: %+v", usage)
	}
	switch record.Mode {
	case ModeDirect:
		if !usage.IsZero() {
			return fmt.Errorf("direct modeはglm-worker委譲を使わないためglm_usageを持ちません: %+v", usage)
		}
	case ModeOrchestrated:
		if usage.Source != GLMUsageSourceTaskStats {
			return fmt.Errorf("orchestrated modeのglm_usage.sourceは%sのみ受理します。任意sourceへの転記は明示的な契約変更が必要です: %q", GLMUsageSourceTaskStats, usage.Source)
		}
		if usage.TaskID == "" {
			return fmt.Errorf("glm_usage.sourceが%sのときtask_idが必要です", GLMUsageSourceTaskStats)
		}
	}
	return nil
}

func validateProxy(proxy ProxyMetrics) error {
	if proxy == (ProxyMetrics{}) {
		return nil
	}
	if proxy.SolPacketBytes < 0 || proxy.SolDecisionCommands < 0 || proxy.SolFixCommands < 0 || proxy.AutoFixRounds < 0 {
		return fmt.Errorf("proxyの値が負です: %+v", proxy)
	}
	return nil
}
