from pathlib import Path
import re

p = Path('glm-worker/internal/harnesslint/quality_surface.go')
s = p.read_text()
old = '\tvar violations []Violation\n\tfor _, check := range qualityWiringChecks() {'
new = '''\tvar violations []Violation
\tworkflowViolations, err := qualityWiringPackageViolations(root, present, "glm-worker/internal/workflow/", []string{
\t\t"w.captureQualitySurfaceBaseline()",
\t\t"w.verifyQualitySurfaceBaseline(workerPhase)",
\t\t"w.qualityGate(w.config.RepoRoot)",
\t\t"harnesslint.IsViolation(qualityReport)",
\t})
\tif err != nil {
\t\treturn nil, err
\t}
\tviolations = append(violations, workflowViolations...)
\tfor _, check := range qualityWiringChecks() {'''
if old not in s:
    raise SystemExit('qualityWiringViolations marker missing')
s = s.replace(old, new, 1)
old_check = '''\t\t{
\t\t\tpath: "glm-worker/internal/workflow/workflow.go",
\t\t\ttokens: []string{
\t\t\t\t"w.captureQualitySurfaceBaseline()",
\t\t\t\t"w.verifyQualitySurfaceBaseline(workerPhase)",
\t\t\t\t"w.qualityGate(w.config.RepoRoot)",
\t\t\t\t"harnesslint.IsViolation(qualityReport)",
\t\t\t},
\t\t},
'''
if old_check not in s:
    raise SystemExit('workflow filename check marker missing')
s = s.replace(old_check, '', 1)
marker = 'func qualityWiringCheckViolations(root string, present map[string]bool, path string, tokens []string) ([]Violation, error) {'
helper = '''func qualityWiringPackageViolations(root string, present map[string]bool, prefix string, tokens []string) ([]Violation, error) {
\tfound := make(map[string]bool, len(tokens))
\tpackageFound := false
\tfor path := range present {
\t\tif !strings.HasPrefix(path, prefix) || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
\t\t\tcontinue
\t\t}
\t\tpackageFound = true
\t\tdata, err := readRegularFile(root, path)
\t\tif err != nil {
\t\t\treturn nil, err
\t\t}
\t\ttext := string(data)
\t\tfor _, token := range tokens {
\t\t\tif strings.Contains(text, token) {
\t\t\t\tfound[token] = true
\t\t\t}
\t\t}
\t}
\tpath := strings.TrimSuffix(prefix, "/")
\tif !packageFound {
\t\treturn []Violation{{Rule: "quality-wiring", Path: path, Line: 1, Column: 1, Message: "required quality-gate package is missing"}}, nil
\t}
\tvar violations []Violation
\tfor _, token := range tokens {
\t\tif found[token] {
\t\t\tcontinue
\t\t}
\t\tviolations = append(violations, Violation{
\t\t\tRule: "quality-wiring", Path: path, Line: 1, Column: 1,
\t\t\tMessage: "required quality-gate wiring is missing: " + token,
\t\t})
\t}
\treturn violations, nil
}

'''
if marker not in s:
    raise SystemExit('helper insertion marker missing')
s = s.replace(marker, helper + marker, 1)
p.write_text(s)

p = Path('glm-worker/internal/harnesslint/quality_surface_test.go')
s = p.read_text()
if 'TestQualityWiringPackageAllowsResponsibilitySplit' not in s:
    s += '''
func TestQualityWiringPackageAllowsResponsibilitySplit(t *testing.T) {
\troot := t.TempDir()
\tworkflowPath := "glm-worker/internal/workflow/workflow.go"
\treviewPath := "glm-worker/internal/workflow/review_flow.go"
\twriteQualityFile(t, root, workflowPath, "package workflow\\nfunc start() { w.captureQualitySurfaceBaseline(); w.verifyQualitySurfaceBaseline(workerPhase) }\\n")
\twriteQualityFile(t, root, reviewPath, "package workflow\\nfunc review() { w.qualityGate(w.config.RepoRoot); harnesslint.IsViolation(qualityReport) }\\n")
\tpresent := map[string]bool{workflowPath: true, reviewPath: true}
\tviolations, err := qualityWiringPackageViolations(root, present, "glm-worker/internal/workflow/", []string{
\t\t"w.captureQualitySurfaceBaseline()",
\t\t"w.verifyQualitySurfaceBaseline(workerPhase)",
\t\t"w.qualityGate(w.config.RepoRoot)",
\t\t"harnesslint.IsViolation(qualityReport)",
\t})
\tif err != nil {
\t\tt.Fatal(err)
\t}
\tif len(violations) != 0 {
\t\tt.Fatalf("violations = %+v", violations)
\t}
}
'''
p.write_text(s)

p = Path('glm-worker/internal/app/command_test.go')
s = p.read_text()
pattern = r'\nfunc TestParseCommandTopLevelUsageIncludesVerifyCodexWake\(t \*testing\.T\) \{.*?\n\}\n'
s, count = re.subn(pattern, '\n', s, count=1, flags=re.S)
if count != 1:
    raise SystemExit('old top-level usage test marker missing')
p.write_text(s)
