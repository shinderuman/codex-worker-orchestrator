from pathlib import Path


def replace_once(path: str, old: str, new: str) -> None:
    file = Path(path)
    text = file.read_text()
    if old not in text:
        raise SystemExit(f"replacement target missing in {path}:\n{old}")
    file.write_text(text.replace(old, new, 1))


replace_once(
    "glm-worker/internal/packet/result.go",
    '''type ParentValidationRequest struct {\n\tForm       string\n\tWorkingDir string\n}\n''',
    '''type ParentValidationRequest struct {\n\tForm       string\n\tWorkingDir string\n}\n\ntype ParentValidationEvidence struct {\n\tValidationRunID string `json:"validation_run_id"`\n\tForm            string `json:"form"`\n\tRepository      string `json:"repository"`\n\tWorkingDir      string `json:"working_dir"`\n\tHead            string `json:"head"`\n\tIndexDigest     string `json:"index_digest"`\n\tWorktreeDigest  string `json:"worktree_digest"`\n\tStatus          string `json:"status"`\n\tExitCode        int    `json:"exit_code"`\n\tDurationMS      int64  `json:"duration_ms"`\n\tLog             string `json:"log"`\n}\n''',
)
replace_once(
    "glm-worker/internal/packet/result.go",
    '''\tParentValidationEvidence   string   `json:"parent_validation_evidence,omitempty"`''',
    '''\tParentValidationEvidence   *ParentValidationEvidence `json:"parent_validation_evidence,omitempty"`''',
)
replace_once(
    "glm-worker/internal/packet/result.go",
    '''func (r *Result) SetParentValidationRequest(request *ParentValidationRequest) {\n\tif request == nil {\n\t\tr.ParentValidation = ""\n\t\tr.ParentValidationWorkingDir = ""\n\t\treturn\n\t}\n\tr.ParentValidation = request.Form\n\tr.ParentValidationWorkingDir = request.WorkingDir\n}\n''',
    '''func (r *Result) SetParentValidationRequest(request *ParentValidationRequest) {\n\tif request == nil {\n\t\tr.ParentValidation = ""\n\t\tr.ParentValidationWorkingDir = ""\n\t\treturn\n\t}\n\tr.ParentValidation = request.Form\n\tr.ParentValidationWorkingDir = request.WorkingDir\n}\n\nfunc (e *ParentValidationEvidence) ResolvedFor(form string) bool {\n\tif e == nil || e.Status != "pass" || e.Form != form {\n\t\treturn false\n\t}\n\tif form != ParentValidationGoTest && form != ParentValidationGoTestRace {\n\t\treturn false\n\t}\n\treturn e.ValidationRunID != "" &&\n\t\te.Repository != "" &&\n\t\te.WorkingDir != "" &&\n\t\te.Head != "" &&\n\t\te.IndexDigest != "" &&\n\t\te.WorktreeDigest != "" &&\n\t\te.Log != ""\n}\n''',
)
replace_once(
    "glm-worker/internal/packet/result.go",
    '''\tif r.Status == StatusImplemented && r.ParentValidationEvidence != "" {\n\t\tobject["parent_validation_evidence"] = r.ParentValidationEvidence\n\t}''',
    '''\tif r.Status == StatusImplemented && r.ParentValidationEvidence != nil {\n\t\tobject["parent_validation_evidence"] = r.ParentValidationEvidence\n\t}''',
)

replace_once(
    "glm-worker/internal/packet/validate.go",
    '''\tif result.ParentValidationEvidence != "" {''',
    '''\tif result.ParentValidationEvidence != nil {''',
)
replace_once(
    "glm-worker/internal/packet/validate.go",
    '''\tif result.ParentValidation != "" || result.ParentValidationWorkingDir != "" || result.ParentValidationEvidence != "" {''',
    '''\tif result.ParentValidation != "" || result.ParentValidationWorkingDir != "" || result.ParentValidationEvidence != nil {''',
)

replace_once(
    "glm-worker/internal/workflow/parent_validation.go",
    '''\tif result.Status != packet.StatusImplemented || request == nil || result.ParentValidationEvidence != "" {''',
    '''\tif result.Status != packet.StatusImplemented || request == nil || result.ParentValidationEvidence != nil {''',
)
replace_once(
    "glm-worker/internal/workflow/parent_validation.go",
    '''\tresult.ParentValidationEvidence = ""''',
    '''\tresult.ParentValidationEvidence = nil''',
)
replace_once(
    "glm-worker/internal/workflow/parent_validation.go",
    '''func parentValidationEvidence(record parentValidationGateRecord) string {\n\treturn fmt.Sprintf(\n\t\t"status=pass;form=%s;validation_run_id=%s;working_dir=%s;head=%s;index=%s;worktree=%s;log=%s",\n\t\trecord.Form,\n\t\trecord.ValidationRunID,\n\t\trecord.WorkingDir,\n\t\trecord.Head,\n\t\trecord.IndexDigest,\n\t\trecord.WorktreeDigest,\n\t\trecord.Log,\n\t)\n}\n''',
    '''func parentValidationEvidence(record parentValidationGateRecord) *packet.ParentValidationEvidence {\n\treturn &packet.ParentValidationEvidence{\n\t\tValidationRunID: record.ValidationRunID,\n\t\tForm:            record.Form,\n\t\tRepository:      record.Repository,\n\t\tWorkingDir:      record.WorkingDir,\n\t\tHead:            record.Head,\n\t\tIndexDigest:     record.IndexDigest,\n\t\tWorktreeDigest:  record.WorktreeDigest,\n\t\tStatus:          record.Status,\n\t\tExitCode:        record.ExitCode,\n\t\tDurationMS:      record.DurationMS,\n\t\tLog:             record.Log,\n\t}\n}\n''',
)

reconciliation = Path("glm-worker/internal/workflow/reviewer_validation_reconciliation.go")
text = reconciliation.read_text()
start = text.index("func reviewerParentValidationResolved(result packet.Result) bool {")
text = text[:start] + '''func reviewerParentValidationResolved(result packet.Result) bool {\n\treturn result.Status == packet.StatusImplemented &&\n\t\tresult.ParentValidationEvidence.ResolvedFor(result.ParentValidation)\n}\n'''
reconciliation.write_text(text)

replace_once(
    "glm-worker/internal/workflow/parent_validation_test.go",
    '''\tif !strings.Contains(r.prompts[2], "parent_validation_evidence") || !strings.Contains(r.prompts[2], "validation_run_id=run-pass") {''',
    '''\tif !strings.Contains(r.prompts[2], "parent_validation_evidence") || !strings.Contains(r.prompts[2], `"validation_run_id":"run-pass"`) {''',
)
