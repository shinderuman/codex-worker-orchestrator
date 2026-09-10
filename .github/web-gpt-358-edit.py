from pathlib import Path


def replace_once(path: str, old: str, new: str) -> None:
    file = Path(path)
    text = file.read_text()
    count = text.count(old)
    if count != 1:
        raise SystemExit(f"{path}: expected exactly one match, got {count}")
    file.write_text(text.replace(old, new, 1))


def create_once(path: str, content: str) -> None:
    file = Path(path)
    if file.exists():
        raise SystemExit(f"{path}: already exists")
    file.write_text(content)


create_once(
    "glm-worker/internal/app/bundle_analysis_review_test.go",
    '''package app

import (
\t"testing"
\t"time"
)

func TestAnalysisSubsequentTurnPropagatesCounterReset(t *testing.T) {
\tstart := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
\tend := start.Add(time.Minute)
\tbaselineInput, baselineCached := int64(100), int64(50)
\tresetInput, resetCached := int64(40), int64(20)
\tscan := bundleRolloutScan{
\t\ttokens: []analysisRolloutTokenAnchor{
\t\t\t{At: start, RawAt: start.Format(time.RFC3339Nano), Offset: 10, Input: &baselineInput, Cached: &baselineCached},
\t\t\t{At: end, RawAt: end.Format(time.RFC3339Nano), Offset: 20, Input: &resetInput, Cached: &resetCached},
\t\t},
\t}
\tturn := analysisRolloutTurn{
\t\tTurnID:      "turn-review-reset",
\t\tStartedAt:   start,
\t\tHasStart:    true,
\t\tCompletedAt: end,
\t\tHasComplete: true,
\t}

\tgot := analysisSubsequentTurn(scan, &turn, end)
\tif got.Status != analysisStatusCounterReset {
\t\tt.Fatalf("status = %q want %q: %#v", got.Status, analysisStatusCounterReset, got)
\t}
\tif got.InputTokens != 0 || got.CachedInputTokens != 0 {
\t\tt.Fatalf("unavailable delta leaked token values: %#v", got)
\t}
\tif got.BaselineAt == "" || got.EndAt == "" {
\t\tt.Fatalf("counter reset lost anchor evidence: %#v", got)
\t}
}

func TestAnalysisAnchoredTokenDeltaMarksMissingEndpointFieldUnknown(t *testing.T) {
\tstart := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
\tend := start.Add(time.Minute)
\tbaselineInput, baselineCached := int64(100), int64(50)
\tendInput := int64(200)
\tscan := bundleRolloutScan{
\t\ttokens: []analysisRolloutTokenAnchor{
\t\t\t{At: start, RawAt: start.Format(time.RFC3339Nano), Offset: 10, Input: &baselineInput, Cached: &baselineCached},
\t\t\t{At: end, RawAt: end.Format(time.RFC3339Nano), Offset: 20, Input: &endInput},
\t\t},
\t}

\tgot := analysisAnchoredTokenDelta(scan, start, end)
\tif got.Status != analysisStatusUnknown {
\t\tt.Fatalf("status = %q want %q: %#v", got.Status, analysisStatusUnknown, got)
\t}
\tif got.InputTokens != 0 || got.CachedInputTokens != 0 {
\t\tt.Fatalf("unknown delta leaked token values: %#v", got)
\t}
\tif got.BaselineAt == "" || got.EndAt == "" {
\t\tt.Fatalf("unknown delta lost anchor evidence: %#v", got)
\t}
}
''',
)

replace_once(
    "glm-worker/internal/app/timeline_test.go",
    '''\tif output.SkippedEvents != 1 {
\t\tt.Fatalf("skipped_events = %d", output.SkippedEvents)
\t}
\tif len(output.Calls) != 1 {
''',
    '''\tif output.SkippedEvents != 1 {
\t\tt.Fatalf("skipped_events = %d", output.SkippedEvents)
\t}
\tif output.Coverage.Status != timelineStatusPartial {
\t\tt.Fatalf("skipped eventを含むcoverage = %#v", output.Coverage)
\t}
\tif len(output.Calls) != 1 {
''',
)

replace_once(
    "glm-worker/internal/app/command_test.go",
    '''func TestParseCommandStdinPayloadModes(t *testing.T) {
''',
    '''func TestParseCommandTopLevelUsageIncludesVerifyCodexWake(t *testing.T) {
\t_, err := ParseCommand(nil)
\tif err == nil || !strings.Contains(err.Error(), "--verify-codex-wake <wake-task-thread-id> <wake-at-rfc3339>") {
\t\tt.Fatalf("top-level usageに--verify-codex-wakeがありません: %v", err)
\t}
}

func TestParseCommandStdinPayloadModes(t *testing.T) {
''',
)

replace_once(
    "glm-worker/internal/app/parent_handoff_test.go",
    '''func TestQualityGateRunRecordCarriesRoutingIdentity(t *testing.T) {
''',
    '''func TestParentHandoffRejectsRoutingEvidenceWithoutTaskID(t *testing.T) {
\tcfg := newAppConfig(t)
\tst, err := state.NewStateStore(cfg)
\tif err != nil {
\t\tt.Fatal(err)
\t}
\tsnapshot, err := state.CaptureGitSnapshot(cfg.RepoRoot)
\tif err != nil {
\t\tt.Fatal(err)
\t}
\trecord := qualityGateRunRecord{
\t\tValidationRunID:               strings.Repeat("b", 32),
\t\tForm:                          "go-test",
\t\tRepository:                    cfg.RepoRoot,
\t\tWorkingDir:                    filepath.Join(cfg.RepoRoot, "module"),
\t\tHead:                          snapshot.Head,
\t\tIndexDigest:                   snapshot.IndexDigest,
\t\tWorktreeDigest:                snapshot.WorktreeDigest,
\t\tWorktreeDigestExcludingParent: snapshot.WorktreeDigestExcludingParent,
\t\tStartedAt:                     time.Now().UTC(),
\t\tStatus:                        qualityGateStatusPass,
\t}
\tif err := writeQualityGateRun(st, record); err != nil {
\t\tt.Fatal(err)
\t}
\tdigest := &state.SnapshotDigest{
\t\tHead:                          snapshot.Head,
\t\tIndexDigest:                   snapshot.IndexDigest,
\t\tWorktreeDigest:                snapshot.WorktreeDigest,
\t\tWorktreeDigestExcludingParent: snapshot.WorktreeDigestExcludingParent,
\t}
\tif got := currentParentRoutingEvidence(st, cfg.RepoRoot, "", digest); len(got) != 0 {
\t\tt.Fatalf("taskless routing evidence leaked: %#v", got)
\t}
}

func TestQualityGateRunRecordCarriesRoutingIdentity(t *testing.T) {
''',
)

replace_once(
    "glm-worker/internal/parentactioncmd/finalization_test.go",
    '''func TestExecuteRejectsInvalidFinalizationForm(t *testing.T) {
''',
    '''func TestFinalizationVerifiedEvidenceDirRejectsLexicallyOutsideMissingPath(t *testing.T) {
\trepo := newFinalizationTestRepo(t)
\toutside := filepath.Join(t.TempDir(), "missing-module")
\tselected, failure := finalizationVerifiedEvidenceDir(repo, finalizationRoutingEvidenceProbe{WorkingDir: outside})
\tif selected != "" {
\t\tt.Fatalf("outside routing evidence selected = %q", selected)
\t}
\tif failure == nil || failure.Stage != "routing" || failure.Reason != "routing_evidence_outside_repository" {
\t\tt.Fatalf("outside missing routing evidence did not fail closed: %#v", failure)
\t}
}

func TestExecuteRejectsInvalidFinalizationForm(t *testing.T) {
''',
)

create_once(
    "glm-worker/internal/harnesslint/external_review_contract_test.go",
    '''package harnesslint

import (
\t"strings"
\t"testing"
)

func TestParentMaintenanceDirectEditAuthorityIsScoped(t *testing.T) {
\tagents := readExecutionPermissionFile(t, "codex", "AGENTS.md")
\tfor _, token := range []string{
\t\t"parent maintenance",
\t\t"parent-managed metadata edit",
\t\t"production code・test・設定・prompt・production wiringの直接編集へ拡張しない",
\t} {
\t\tif !strings.Contains(agents, token) {
\t\t\tt.Fatalf("codex/AGENTS.md missing parent-maintenance authority token %q", token)
\t\t}
\t}
}

func TestIsolationInstructionRetainsBranchUntilOriginalTaskCompletes(t *testing.T) {
\tcontract := readExecutionPermissionFile(t, "codex", "instructions", "glm-stop-isolate.md")
\tfor _, token := range []string{
\t\t"隔離branch",
\t\t"元taskのresume保持照合が完了し元taskが完了するまで削除しない",
\t} {
\t\tif !strings.Contains(contract, token) {
\t\t\tt.Fatalf("glm-stop-isolate.md missing branch lifetime token %q", token)
\t\t}
\t}
}
''',
)
