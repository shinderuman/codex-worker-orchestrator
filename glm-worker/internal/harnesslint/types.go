package harnesslint

import "sort"

type Violation struct {
	Rule    string `json:"rule"`
	Path    string `json:"path"`
	Line    int    `json:"line"`
	Column  int    `json:"column"`
	Message string `json:"message"`
	Fixable bool   `json:"fixable"`
}

type FixInputSnapshot struct {
	Head           string `json:"head"`
	IndexDigest    string `json:"index_digest"`
	WorktreeDigest string `json:"worktree_digest"`
}

type FixEvidence struct {
	Method string            `json:"method"`
	Input  *FixInputSnapshot `json:"input,omitempty"`
}

type Report struct {
	Status      string       `json:"status"`
	Fixed       int          `json:"fixed"`
	Violations  []Violation  `json:"violations"`
	FixEvidence *FixEvidence `json:"fix_evidence,omitempty"`
}

const FixProvenanceIsolatedPostimageV1 = "isolated-postimage-v1"

func makeReport(fixed int, violations []Violation) Report {
	sort.Slice(violations, func(i, j int) bool {
		if violations[i].Path != violations[j].Path {
			return violations[i].Path < violations[j].Path
		}
		if violations[i].Line != violations[j].Line {
			return violations[i].Line < violations[j].Line
		}
		if violations[i].Column != violations[j].Column {
			return violations[i].Column < violations[j].Column
		}
		return violations[i].Rule < violations[j].Rule
	})
	status := "pass"
	if len(violations) > 0 {
		status = "fail"
	}
	return Report{Status: status, Fixed: fixed, Violations: violations}
}

func IsViolation(report Report) bool {
	return report.Status == "fail" && len(report.Violations) > 0
}
