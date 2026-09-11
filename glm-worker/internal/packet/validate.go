package packet

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

type constraintError struct {
	reason   string
	reasons  []string
	keys     []string
	category string
}

type constraintCollector struct {
	reasons  []string
	keys     []string
	seen     map[string]struct{}
	omitted  int
	category string
}

const maxConstraintReasons = 16

func (e *constraintError) Error() string {
	return e.reason
}

func newConstraintError(key, reason string) *constraintError {
	return &constraintError{reason: reason, reasons: []string{reason}, keys: []string{key}, category: rejectCategoryForKey(key)}
}

func IsConstraintError(err error) bool {
	var target *constraintError
	return errors.As(err, &target)
}

func ConstraintReasons(err error) []string {
	var target *constraintError
	if !errors.As(err, &target) {
		return nil
	}
	if len(target.reasons) != 0 {
		return append([]string(nil), target.reasons...)
	}
	if target.reason == "" {
		return nil
	}
	return []string{target.reason}
}

func ConstraintViolationKeys(err error) []string {
	var target *constraintError
	if !errors.As(err, &target) {
		return nil
	}
	if len(target.keys) != 0 {
		return append([]string(nil), target.keys...)
	}
	if target.reason == "" {
		return nil
	}
	return []string{target.reason}
}

func newConstraintCollector() *constraintCollector {
	return &constraintCollector{seen: make(map[string]struct{})}
}

func (c *constraintCollector) add(err error) error {
	if err == nil {
		return nil
	}
	reasons := ConstraintReasons(err)
	keys := ConstraintViolationKeys(err)
	if len(reasons) == 0 || len(keys) != len(reasons) {
		return err
	}
	for index, reason := range reasons {
		c.addReason(keys[index], reason)
	}
	return nil
}

func (c *constraintCollector) addReason(key, reason string) {
	if key == "" || reason == "" {
		return
	}
	if _, exists := c.seen[reason]; exists {
		return
	}
	c.seen[reason] = struct{}{}
	if c.category == "" {
		c.category = rejectCategoryForKey(key)
	}
	if len(c.reasons) >= maxConstraintReasons {
		c.omitted++
		return
	}
	c.reasons = append(c.reasons, reason)
	c.keys = append(c.keys, key)
}

func (c *constraintCollector) err() error {
	if len(c.reasons) == 0 {
		return nil
	}
	display := append([]string(nil), c.reasons...)
	if c.omitted != 0 {
		display = append(display, fmt.Sprintf("ほか%d件の独立した違反があります", c.omitted))
	}
	return &constraintError{
		reason:   strings.Join(display, "; "),
		reasons:  append([]string(nil), c.reasons...),
		keys:     append([]string(nil), c.keys...),
		category: c.category,
	}
}

func rejectCategoryForKey(key string) string {
	switch {
	case strings.HasPrefix(key, "artifact"):
		return "artifacts"
	case strings.HasPrefix(key, "multiline-field"), key == "list-element-multiline":
		return "multiline-field"
	case strings.HasPrefix(key, "field-size"), key == "list-element-size", key == "packet-size":
		return "size"
	case strings.HasPrefix(key, "missing-field"):
		return "missing-field"
	case strings.HasPrefix(key, "targets"):
		return "targets-none"
	case strings.Contains(key, "risk"):
		return "risk"
	case strings.Contains(key, "status"):
		return "status"
	default:
		return "other"
	}
}

func RejectCategory(err error) string {
	if err == nil {
		return ""
	}
	if IsMismatchError(err) {
		return "schema-mismatch"
	}
	var constraint *constraintError
	if errors.As(err, &constraint) && constraint.category != "" {
		return constraint.category
	}
	return rejectCategoryForMessage(strings.ToLower(err.Error()))
}

func rejectCategoryForMessage(msg string) string {
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
	collector := newConstraintCollector()
	if err := collector.add(validateMachineStatusRisk(result, workerMachineContract)); err != nil {
		return err
	}
	if err := collector.add(validateParentValidation(result)); err != nil {
		return err
	}
	if err := collector.add(validateFields(result, resultFieldsForStatus(result.Status))); err != nil {
		return err
	}
	if err := collector.add(validateTargets(result)); err != nil {
		return err
	}
	return collector.err()
}

func validateParentValidation(result Result) error {
	if result.ParentValidationEvidence != nil {
		return newConstraintError("parent-validation-evidence", "parent_validation_evidenceはwrapper専用fieldです")
	}
	if result.ParentValidation == "" && result.ParentValidationWorkingDir == "" {
		return nil
	}
	if result.Status != StatusImplemented {
		return newConstraintError("parent-validation-status", "parent_validationはIMPLEMENTEDだけで指定できます")
	}
	if result.ParentValidation == "" || result.ParentValidationWorkingDir == "" {
		return newConstraintError("parent-validation-pair", "parent_validationとparent_validation_working_dirは同時に指定してください")
	}
	if !validParentValidationForm(result.ParentValidation) {
		return newConstraintError("parent-validation-form", fmt.Sprintf("parent_validationは既知のparent gateだけを指定してください: %q", result.ParentValidation))
	}
	return validateParentValidationWorkingDir(result.ParentValidationWorkingDir)
}

func validParentValidationForm(form string) bool {
	return form == ParentValidationGoTest || form == ParentValidationGoTestRace
}

func validateParentValidationWorkingDir(workingDir string) error {
	invalid := path.IsAbs(workingDir) ||
		strings.Contains(workingDir, "\\") ||
		path.Clean(workingDir) != workingDir ||
		workingDir == ".." || strings.HasPrefix(workingDir, "../")
	if invalid {
		return newConstraintError("parent-validation-working-dir", fmt.Sprintf("parent_validation_working_dirは正規化済みrepository相対pathで指定してください: %q", workingDir))
	}
	return nil
}

func ValidateReviewerResult(result Result) error {
	collector := newConstraintCollector()
	if err := collector.add(validateMachineStatusRisk(result, reviewerMachineContract)); err != nil {
		return err
	}
	if err := collector.add(validateReviewerParentValidation(result)); err != nil {
		return err
	}
	if err := collector.add(validateFields(result, resultFieldsForStatus(result.Status))); err != nil {
		return err
	}
	if err := collector.add(validateTargets(result)); err != nil {
		return err
	}
	return collector.err()
}

func validateReviewerParentValidation(result Result) error {
	if result.ParentValidation != "" || result.ParentValidationWorkingDir != "" || result.ParentValidationEvidence != nil {
		return newConstraintError("reviewer-parent-validation", "reviewer結果にparent validation fieldは指定できません")
	}
	return nil
}

func validateTargets(result Result) error {
	if len(result.Targets) == 0 {
		if result.Status == StatusImplemented {
			return nil
		}
		return newConstraintError("targets-empty", fmt.Sprintf("%sのTARGETSは空にできません: Solが読むべき最小対象をfile:symbol/行範囲で指定してください", string(result.Status)))
	}
	collector := newConstraintCollector()
	seen := make(map[string]struct{}, len(result.Targets))
	hasNone := false
	for _, element := range result.Targets {
		isNone, err := validateTargetElement(result, element, seen)
		if collectErr := collector.add(err); collectErr != nil {
			return collectErr
		}
		hasNone = hasNone || isNone
	}
	if err := collector.add(validateNoneTarget(result, hasNone)); err != nil {
		return err
	}
	return collector.err()
}

func validateTargetElement(result Result, element string, seen map[string]struct{}) (bool, error) {
	trimmed := strings.TrimSpace(element)
	if trimmed == "" {
		return false, newConstraintError("targets-element-empty", "TARGETSの要素は空・空白のみにできません: 具体対象または予約値none/PACKETを指定してください")
	}
	if _, duplicate := seen[trimmed]; duplicate {
		return false, newConstraintError("targets-duplicate", "TARGETSの要素が重複しています: 各対象は1回だけ指定してください")
	}
	seen[trimmed] = struct{}{}
	if strings.EqualFold(trimmed, noneTargetsSentinel) {
		if element != noneTargetsSentinel {
			return false, newConstraintError("targets-none-case", "TARGETSの予約値noneは小文字厳密表現のnoneだけを要素にできます: 大小文字・空白の変形は使えません")
		}
		return true, nil
	}
	if strings.EqualFold(trimmed, ReportOnlyTargets) &&
		(result.Status != StatusFixRequired || element != ReportOnlyTargets || len(result.Targets) != 1) {
		return false, newConstraintError("targets-packet-reserved", "TARGETSの予約値PACKETはFIX_REQUIREDの報告再出力専用です: 実装修正では具体対象を指定してください")
	}
	return false, nil
}

func validateNoneTarget(result Result, hasNone bool) error {
	if !hasNone {
		return nil
	}
	if len(result.Targets) > 1 {
		return newConstraintError("targets-none-mixed", "TARGETSの予約値noneは具体対象と混在できません: 対象が概念的なときはnoneだけを要素にしてください")
	}
	if result.Status == StatusNeedsSolReview {
		return newConstraintError("targets-none-review", "NEEDS_SOL_REVIEWのTARGETSはnoneにできません: Solが読むべき最小対象をfile:symbol/行範囲で指定してください")
	}
	return nil
}

func validateFields(result Result, fields []machineField) error {
	collector := newConstraintCollector()
	for _, field := range fields {
		value := machineFieldValue(result, field)
		if strings.TrimSpace(value) == "" {
			collector.addReason("missing-field:"+string(field), fmt.Sprintf("結果に必須field %sがありません", field))
			continue
		}
		if strings.ContainsAny(value, "\n\r") {
			collector.addReason("multiline-field:"+string(field), fmt.Sprintf("field %sに改行を含められません: 複数事項は同じvalue内でセミコロン区切りにしてください", field))
		}
		if len(value) > MaxFieldBytes {
			collector.addReason("field-size:"+string(field), fmt.Sprintf("field %sは%d bytes以内にしてください", field, MaxFieldBytes))
		}
	}
	for _, value := range append(append([]string(nil), result.Targets...), result.Artifacts...) {
		if strings.ContainsAny(value, "\n\r") {
			collector.addReason("list-element-multiline", "TARGETS/ARTIFACTSの各要素に改行を含められません")
		}
		if len(value) > MaxFieldBytes {
			collector.addReason("list-element-size", fmt.Sprintf("TARGETS/ARTIFACTSの各要素は%d bytes以内にしてください", MaxFieldBytes))
		}
	}
	if size := result.ByteSize(); size > MaxPacketBytes {
		collector.addReason("packet-size", fmt.Sprintf("結果全体はmachine JSONで%d bytes以内にしてください: %d bytes", MaxPacketBytes, size))
	}
	return collector.err()
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
		return newConstraintError("artifact-root", fmt.Sprintf("artifact rootを確認できません: %v", err))
	}
	collector := newConstraintCollector()
	seen := make(map[string]struct{})
	for _, path := range artifacts {
		if err := collector.add(validateArtifactPath(path, root, resolvedRoot, seen)); err != nil {
			return err
		}
	}
	return collector.err()
}

func validateArtifactPath(path, root, resolvedRoot string, seen map[string]struct{}) error {
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return newConstraintError("artifact-path", fmt.Sprintf("ARTIFACTSは正規化済み絶対パスを指定してください: %q", path))
	}
	if !pathWithinRoot(root, path) {
		return newConstraintError("artifact-outside-root", fmt.Sprintf("ARTIFACTSは現在taskのartifact dir配下だけを指定してください: %s", path))
	}
	if _, exists := seen[path]; exists {
		return newConstraintError("artifact-duplicate", fmt.Sprintf("ARTIFACTSのパスが重複しています: %s", path))
	}
	seen[path] = struct{}{}

	info, err := os.Lstat(path)
	if err != nil {
		return newConstraintError("artifact-file", fmt.Sprintf("ARTIFACTSのファイルを確認できません: %s: %v", path, err))
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return newConstraintError("artifact-regular-file", fmt.Sprintf("ARTIFACTSは実在する通常ファイルだけを指定してください: %s", path))
	}
	resolvedPath, err := filepath.EvalSymlinks(path)
	if err != nil || !pathWithinRoot(resolvedRoot, resolvedPath) {
		return newConstraintError("artifact-resolved-outside-root", fmt.Sprintf("ARTIFACTSの解決先がartifact dir外です: %s", path))
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
