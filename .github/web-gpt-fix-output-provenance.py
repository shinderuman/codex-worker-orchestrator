from pathlib import Path


def replace_once(path: str, old: str, new: str) -> None:
    p = Path(path)
    text = p.read_text()
    if text.count(old) != 1:
        raise SystemExit(f"{path}: replacement count for marker is {text.count(old)}")
    p.write_text(text.replace(old, new, 1))


replace_once(
    "glm-worker/internal/harnesslint/types.go",
    '''type FixEvidence struct {
\tMethod string            `json:"method"`
\tInput  *FixInputSnapshot `json:"input,omitempty"`
}
''',
    '''type FixEvidence struct {
\tMethod string            `json:"method"`
\tInput  *FixInputSnapshot `json:"input,omitempty"`
\tOutput *FixInputSnapshot `json:"output,omitempty"`
}
''',
)

replace_once(
    "glm-worker/internal/harnesslint/fix_isolation.go",
    '''\tif err := applyIsolatedFixPostimages(root, before, after, changed); err != nil {
\t\treturn Report{}, err
\t}
\treport.FixEvidence = &FixEvidence{
\t\tMethod: FixProvenanceIsolatedPostimageV1,
\t\tInput: &FixInputSnapshot{
\t\t\tHead:           input.Head,
\t\t\tIndexDigest:    input.IndexDigest,
\t\t\tWorktreeDigest: input.WorktreeDigest,
\t\t},
\t}
''',
    '''\toutput, err := applyIsolatedFixPostimages(root, before, after, changed)
\tif err != nil {
\t\treturn Report{}, err
\t}
\treport.FixEvidence = &FixEvidence{
\t\tMethod: FixProvenanceIsolatedPostimageV1,
\t\tInput: &FixInputSnapshot{
\t\t\tHead:           input.Head,
\t\t\tIndexDigest:    input.IndexDigest,
\t\t\tWorktreeDigest: input.WorktreeDigest,
\t\t},
\t\tOutput: &FixInputSnapshot{
\t\t\tHead:           output.Head,
\t\t\tIndexDigest:    output.IndexDigest,
\t\t\tWorktreeDigest: output.WorktreeDigest,
\t\t},
\t}
''',
)

replace_once(
    "glm-worker/internal/harnesslint/fix_isolation.go",
    '''func applyIsolatedFixPostimages(root string, before, after fixManifest, changed []string) error {
\tif err := verifyFixManifest(root, before); err != nil {
\t\treturn fmt.Errorf("quality fixer input changed while isolated fixer ran: %w", err)
\t}
\tfor _, path := range changed {
\t\tif err := applyFixPostimage(root, path, before[path], after[path]); err != nil {
\t\t\treturn err
\t\t}
\t}
\tif err := verifyFixManifest(root, after); err != nil {
\t\treturn fmt.Errorf("quality fixer postimage verification failed: %w", err)
\t}
\treturn nil
}
''',
    '''func applyIsolatedFixPostimages(root string, before, after fixManifest, changed []string) (state.GitSnapshot, error) {
\tif err := verifyFixManifest(root, before); err != nil {
\t\treturn state.GitSnapshot{}, fmt.Errorf("quality fixer input changed while isolated fixer ran: %w", err)
\t}
\tfor _, path := range changed {
\t\tif err := applyFixPostimage(root, path, before[path], after[path]); err != nil {
\t\t\treturn state.GitSnapshot{}, err
\t\t}
\t}
\toutput, err := state.CaptureGitSnapshot(root)
\tif err != nil {
\t\treturn state.GitSnapshot{}, err
\t}
\tif err := verifyFixManifest(root, after); err != nil {
\t\treturn state.GitSnapshot{}, fmt.Errorf("quality fixer postimage verification failed: %w", err)
\t}
\treturn output, nil
}
''',
)

replace_once(
    "glm-worker/internal/workflow/review_flow.go",
    '''\tcase evidence.Input.Head != workerEnd.Head || evidence.Input.IndexDigest != workerEnd.IndexDigest || evidence.Input.WorktreeDigest != workerEnd.WorktreeDigest:
\t\treturn fmt.Sprintf("machine quality fixer provenanceがworker-end snapshotと一致しません(fixed=%d)", report.Fixed)
\tcase reviewInput.Head != workerEnd.Head || reviewInput.IndexDigest != workerEnd.IndexDigest:
\t\treturn fmt.Sprintf("machine quality fixer実行中にHEAD/indexが変化しました(fixed=%d)", report.Fixed)
''',
    '''\tcase evidence.Input.Head != workerEnd.Head || evidence.Input.IndexDigest != workerEnd.IndexDigest || evidence.Input.WorktreeDigest != workerEnd.WorktreeDigest:
\t\treturn fmt.Sprintf("machine quality fixer input provenanceがworker-end snapshotと一致しません(fixed=%d)", report.Fixed)
\tcase evidence.Output == nil:
\t\treturn fmt.Sprintf("machine quality fixer output provenanceがありません(fixed=%d)", report.Fixed)
\tcase evidence.Output.Head != reviewInput.Head || evidence.Output.IndexDigest != reviewInput.IndexDigest || evidence.Output.WorktreeDigest != reviewInput.WorktreeDigest:
\t\treturn fmt.Sprintf("machine quality fixer output provenanceがreview-input snapshotと一致しません(fixed=%d)", report.Fixed)
\tcase reviewInput.Head != workerEnd.Head || reviewInput.IndexDigest != workerEnd.IndexDigest:
\t\treturn fmt.Sprintf("machine quality fixer実行中にHEAD/indexが変化しました(fixed=%d)", report.Fixed)
''',
)

replace_once(
    "glm-worker/internal/workflow/quality_fixer_snapshot_test.go",
    '''\t\tw.qualityGate = func(root string) (harnesslint.Report, error) {
\t\tinput, err := state.CaptureGitSnapshot(root)
\t\tif err != nil {
\t\t\treturn harnesslint.Report{}, err
\t\t}
\t\tif err := os.WriteFile(path, formatted, 0o644); err != nil {
\t\t\treturn harnesslint.Report{}, err
\t\t}
\t\treturn qualityFixReportForSnapshot(input), nil
\t}
'''.replace('\t\t','\t',1),
    '''\tw.qualityGate = func(root string) (harnesslint.Report, error) {
\t\tinput, err := state.CaptureGitSnapshot(root)
\t\tif err != nil {
\t\t\treturn harnesslint.Report{}, err
\t\t}
\t\tif err := os.WriteFile(path, formatted, 0o644); err != nil {
\t\t\treturn harnesslint.Report{}, err
\t\t}
\t\toutput, err := state.CaptureGitSnapshot(root)
\t\tif err != nil {
\t\t\treturn harnesslint.Report{}, err
\t\t}
\t\treturn qualityFixReportForSnapshots(input, output), nil
\t}
''',
)

replace_once(
    "glm-worker/internal/workflow/quality_fixer_snapshot_test.go",
    '''func qualityFixReportForSnapshot(input state.GitSnapshot) harnesslint.Report {
\treturn harnesslint.Report{
\t\tStatus:     "pass",
\t\tFixed:      1,
\t\tViolations: []harnesslint.Violation{},
\t\tFixEvidence: &harnesslint.FixEvidence{
\t\t\tMethod: harnesslint.FixProvenanceIsolatedPostimageV1,
\t\t\tInput: &harnesslint.FixInputSnapshot{
\t\t\t\tHead:           input.Head,
\t\t\t\tIndexDigest:    input.IndexDigest,
\t\t\t\tWorktreeDigest: input.WorktreeDigest,
\t\t\t},
\t\t},
\t}
}
''',
    '''func qualityFixReportForSnapshots(input, output state.GitSnapshot) harnesslint.Report {
\treturn harnesslint.Report{
\t\tStatus:     "pass",
\t\tFixed:      1,
\t\tViolations: []harnesslint.Violation{},
\t\tFixEvidence: &harnesslint.FixEvidence{
\t\t\tMethod: harnesslint.FixProvenanceIsolatedPostimageV1,
\t\t\tInput: &harnesslint.FixInputSnapshot{
\t\t\t\tHead:           input.Head,
\t\t\t\tIndexDigest:    input.IndexDigest,
\t\t\t\tWorktreeDigest: input.WorktreeDigest,
\t\t\t},
\t\t\tOutput: &harnesslint.FixInputSnapshot{
\t\t\t\tHead:           output.Head,
\t\t\t\tIndexDigest:    output.IndexDigest,
\t\t\t\tWorktreeDigest: output.WorktreeDigest,
\t\t\t},
\t\t},
\t}
}

func TestQualityFixSnapshotRejectsOutputMismatch(t *testing.T) {
\tworkerEnd := state.GitSnapshot{Head: "head", IndexDigest: "index", WorktreeDigest: "before"}
\treviewInput := state.GitSnapshot{Head: "head", IndexDigest: "index", WorktreeDigest: "after-external"}
\treport := qualityFixReportForSnapshots(workerEnd, state.GitSnapshot{Head: "head", IndexDigest: "index", WorktreeDigest: "fixer-after"})
\tif reason := qualityFixSnapshotMismatchReason(workerEnd, reviewInput, report); reason == "" {
\t\tt.Fatal("fixer return後のexternal worktree changeを受理しています")
\t}
}
''',
)

replace_once(
    "glm-worker/internal/harnesslint/fix_isolation_test.go",
    '''\tif report.FixEvidence == nil || report.FixEvidence.Method != FixProvenanceIsolatedPostimageV1 {
\t\tt.Fatalf("fix evidence = %#v", report.FixEvidence)
\t}
''',
    '''\tif report.FixEvidence == nil || report.FixEvidence.Method != FixProvenanceIsolatedPostimageV1 || report.FixEvidence.Input == nil || report.FixEvidence.Output == nil {
\t\tt.Fatalf("fix evidence = %#v", report.FixEvidence)
\t}
\tif report.FixEvidence.Input.WorktreeDigest == report.FixEvidence.Output.WorktreeDigest {
\t\tt.Fatal("fix evidence input/output worktree digests did not change")
\t}
''',
)
