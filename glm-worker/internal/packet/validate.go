package packet

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type constraintError struct {
	reason string
}

func (e *constraintError) Error() string {
	return e.reason
}

func IsConstraintError(err error) bool {
	var target *constraintError
	return errors.As(err, &target)
}

func RejectCategory(err error) string {
	if err == nil {
		return ""
	}
	if IsMismatchError(err) {
		return "schema-mismatch"
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "artifacts") || strings.Contains(msg, "artifact"):
		return "artifacts"
	case strings.Contains(msg, "改行"):
		return "multiline-field"
	case strings.Contains(msg, "bytes以内"):
		return "size"
	case strings.Contains(msg, "必須field"):
		return "missing-field"
	case strings.Contains(msg, "targets"):
		return "targets-none"
	case strings.Contains(msg, "risk"):
		return "risk"
	case strings.Contains(msg, "status"):
		return "status"
	default:
		return "other"
	}
}

func ValidateWorkerResult(result Result) error {
	switch result.Status {
	case StatusImplemented:
		if result.Risk != RiskLow && result.Risk != RiskHigh {
			return &constraintError{reason: fmt.Sprintf("riskはLOWまたはHIGHで指定してください: %q", string(result.Risk))}
		}
	case StatusNeedsSolDecision:
		if result.Risk != RiskHigh {
			return &constraintError{reason: "NEEDS_SOL_DECISIONのriskはHIGHにしてください"}
		}
	default:
		return &mismatchError{reason: fmt.Sprintf("worker結果のstatusとして許容されません: %q", string(result.Status))}
	}
	if err := validateParentValidation(result); err != nil {
		return err
	}
	if err := validateFields(result, result.contractFields()); err != nil {
		return err
	}
	return validateTargets(result)
}

func validateParentValidation(result Result) error {
	if result.ParentValidationEvidence != "" {
		return &constraintError{reason: "parent_validation_evidenceはwrapper専用fieldです"}
	}
	if result.ParentValidation == "" {
		return nil
	}
	if result.Status != StatusImplemented {
		return &constraintError{reason: "parent_validationはIMPLEMENTEDだけで指定できます"}
	}
	switch result.ParentValidation {
	case ParentValidationGoTest, ParentValidationGoTestRace:
		return nil
	default:
		return &constraintError{reason: fmt.Sprintf("parent_validationは既知のparent gateだけを指定してください: %q", result.ParentValidation)}
	}
}

func ValidateReviewerResult(result Result) error {
	switch result.Status {
	case StatusPass:
		if result.Risk != RiskLow {
			return &constraintError{reason: "PASSのriskはLOWにしてください。高リスクならNEEDS_SOL_REVIEWを返してください"}
		}
	case StatusFixRequired:
		if result.Risk != RiskLow && result.Risk != RiskHigh {
			return &constraintError{reason: fmt.Sprintf("riskはLOWまたはHIGHで指定してください: %q", string(result.Risk))}
		}
	case StatusNeedsSolReview:
		if result.Risk != RiskHigh {
			return &constraintError{reason: "NEEDS_SOL_REVIEWのriskはHIGHにしてください"}
		}
	case StatusNeedsSolDecision:
		if result.Risk != RiskHigh {
			return &constraintError{reason: "NEEDS_SOL_DECISIONのriskはHIGHにしてください"}
		}
	default:
		return &mismatchError{reason: fmt.Sprintf("reviewer結果のstatusとして許容されません: %q", string(result.Status))}
	}
	if result.ParentValidation != "" || result.ParentValidationEvidence != "" {
		return &constraintError{reason: "reviewer結果にparent validation fieldは指定できません"}
	}
	if err := validateFields(result, result.contractFields()); err != nil {
		return err
	}
	return validateTargets(result)
}

func validateTargets(result Result) error {
	if len(result.Targets) == 0 {
		if result.Status == StatusImplemented {
			return nil
		}
		return &constraintError{reason: fmt.Sprintf("%sのTARGETSは空にできません: Solが読むべき最小対象をfile:symbol/行範囲で指定してください", string(result.Status))}
	}
	seen := make(map[string]struct{}, len(result.Targets))
	hasNone := false
	for _, element := range result.Targets {
		isNone, err := validateTargetElement(result, element, seen)
		if err != nil {
			return err
		}
		hasNone = hasNone || isNone
	}
	return validateNoneTarget(result, hasNone)
}

func validateTargetElement(result Result, element string, seen map[string]struct{}) (bool, error) {
	trimmed := strings.TrimSpace(element)
	if trimmed == "" {
		return false, &constraintError{reason: "TARGETSの要素は空・空白のみにできません: 具体対象または予約値none/PACKETを指定してください"}
	}
	if _, duplicate := seen[trimmed]; duplicate {
		return false, &constraintError{reason: "TARGETSの要素が重複しています: 各対象は1回だけ指定してください"}
	}
	seen[trimmed] = struct{}{}
	if strings.EqualFold(trimmed, noneTargetsSentinel) {
		if element != noneTargetsSentinel {
			return false, &constraintError{reason: "TARGETSの予約値noneは小文字厳密表現のnoneだけを要素にできます: 大小文字・空白の変形は使えません"}
		}
		return true, nil
	}
	if strings.EqualFold(trimmed, ReportOnlyTargets) &&
		(result.Status != StatusFixRequired || element != ReportOnlyTargets || len(result.Targets) != 1) {
		return false, &constraintError{reason: "TARGETSの予約値PACKETはFIX_REQUIREDの報告再出力専用です: 実装修正では具体対象を指定してください"}
	}
	return false, nil
}

func validateNoneTarget(result Result, hasNone bool) error {
	if !hasNone {
		return nil
	}
	if len(result.Targets) > 1 {
		return &constraintError{reason: "TARGETSの予約値noneは具体対象と混在できません: 対象が概念的なときはnoneだけを要素にしてください"}
	}
	if result.Status == StatusNeedsSolReview {
		return &constraintError{reason: "NEEDS_SOL_REVIEWのTARGETSはnoneにできません: Solが読むべき最小対象をfile:symbol/行範囲で指定してください"}
	}
	return nil
}

func validateFields(result Result, fields []contractField) error {
	for _, field := range fields {
		value := field.value(result)
		if strings.TrimSpace(value) == "" {
			return &constraintError{reason: fmt.Sprintf("結果に必須field %sがありません", field.machine)}
		}
		if strings.ContainsAny(value, "\n\r") {
			return &constraintError{reason: fmt.Sprintf("field %sに改行を含められません: 複数事項は同じvalue内でセミコロン区切りにしてください", field.machine)}
		}
		if len(value) > MaxFieldBytes {
			return &constraintError{reason: fmt.Sprintf("field %sは%d bytes以内にしてください", field.machine, MaxFieldBytes)}
		}
	}
	for _, value := range append(append([]string(nil), result.Targets...), result.Artifacts...) {
		if strings.ContainsAny(value, "\n\r") {
			return &constraintError{reason: "TARGETS/ARTIFACTSの各要素に改行を含められません"}
		}
		if len(value) > MaxFieldBytes {
			return &constraintError{reason: fmt.Sprintf("TARGETS/ARTIFACTSの各要素は%d bytes以内にしてください", MaxFieldBytes)}
		}
	}
	if size := result.ByteSize(); size > MaxPacketBytes {
		return &constraintError{reason: fmt.Sprintf("結果全体はmachine JSONで%d bytes以内にしてください: %d bytes", MaxPacketBytes, size)}
	}
	return nil
}

func IsReportOnlyFix(result Result) bool {
	return result.Status == StatusFixRequired && len(result.Targets) == 1 && result.Targets[0] == ReportOnlyTargets
}

func ValidateArtifacts(artifacts []string, root string) error {
	if len(artifacts) == 0 {
		return nil
	}

	root = filepath.Clean(root)
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return &constraintError{reason: fmt.Sprintf("artifact rootを確認できません: %v", err)}
	}
	seen := make(map[string]struct{})
	for _, path := range artifacts {
		if err := validateArtifactPath(path, root, resolvedRoot, seen); err != nil {
			return err
		}
	}
	return nil
}

func validateArtifactPath(path, root, resolvedRoot string, seen map[string]struct{}) error {
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return &constraintError{reason: fmt.Sprintf("ARTIFACTSは正規化済み絶対パスを指定してください: %q", path)}
	}
	if !pathWithinRoot(root, path) {
		return &constraintError{reason: fmt.Sprintf("ARTIFACTSは現在taskのartifact dir配下だけを指定してください: %s", path)}
	}
	if _, exists := seen[path]; exists {
		return &constraintError{reason: fmt.Sprintf("ARTIFACTSのパスが重複しています: %s", path)}
	}
	seen[path] = struct{}{}

	info, err := os.Lstat(path)
	if err != nil {
		return &constraintError{reason: fmt.Sprintf("ARTIFACTSのファイルを確認できません: %s: %v", path, err)}
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return &constraintError{reason: fmt.Sprintf("ARTIFACTSは実在する通常ファイルだけを指定してください: %s", path)}
	}
	resolvedPath, err := filepath.EvalSymlinks(path)
	if err != nil || !pathWithinRoot(resolvedRoot, resolvedPath) {
		return &constraintError{reason: fmt.Sprintf("ARTIFACTSの解決先がartifact dir外です: %s", path)}
	}
	return nil
}

func pathWithinRoot(root string, path string) bool {
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == "." || relative == ".." {
		return false
	}
	return !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
