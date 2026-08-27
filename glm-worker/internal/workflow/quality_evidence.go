package workflow

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/scanner"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type qualityEvidenceDecision struct {
	High    bool
	Source  string
	HitPath string
}

type parentBehaviorEvalRegistry struct {
	Cases []parentBehaviorEvalCase `json:"cases"`
}

type parentBehaviorEvalCase struct {
	ID              string   `json:"id"`
	ContractSources []string `json:"contract_sources"`
	Positive        string   `json:"positive"`
	Negative        string   `json:"negative"`
	Evidence        string   `json:"evidence"`
	RunPolicy       string   `json:"run_policy"`
}

const parentBehaviorEvalPath = "tests/parent-behavior-evals.json"

func classifyQualityEvidence(repoRoot, baselineHead string, paths []string) (qualityEvidenceDecision, error) {
	if strings.TrimSpace(baselineHead) == "" || !hasQualityEvidenceChanges(paths) {
		return qualityEvidenceDecision{}, nil
	}
	if err := verifyQualityEvidenceBaseline(repoRoot, baselineHead); err != nil {
		return qualityEvidenceDecision{}, err
	}
	before := map[string]int{}
	after := map[string]int{}
	decision, err := classifyChangedQualityEvidence(repoRoot, baselineHead, paths, before, after)
	if err != nil || decision.High {
		return decision, err
	}
	if missing := firstMissingEvidenceSignature(before, after); missing != "" {
		return qualityEvidenceDecision{High: true, Source: "track-a-evidence-removed", HitPath: missing}, nil
	}
	return qualityEvidenceDecision{}, nil
}

func classifyChangedQualityEvidence(
	repoRoot, baselineHead string,
	paths []string,
	before, after map[string]int,
) (qualityEvidenceDecision, error) {
	for _, path := range paths {
		decision, err := classifyQualityEvidencePath(repoRoot, baselineHead, path, before, after)
		if err != nil || decision.High {
			return decision, err
		}
	}
	return qualityEvidenceDecision{}, nil
}

func classifyQualityEvidencePath(
	repoRoot, baselineHead, path string,
	before, after map[string]int,
) (qualityEvidenceDecision, error) {
	decision, handled, err := classifyRepositoryQualityEvidence(repoRoot, baselineHead, path)
	if err != nil {
		return qualityEvidenceDecision{}, err
	}
	if handled {
		return decision, nil
	}
	if !isGenericQualityEvidencePath(path) {
		return qualityEvidenceDecision{}, nil
	}
	if err := collectQualityEvidenceSignatures(repoRoot, baselineHead, path, before, after); err != nil {
		return qualityEvidenceDecision{}, err
	}
	return qualityEvidenceDecision{}, nil
}

func hasQualityEvidenceChanges(paths []string) bool {
	for _, path := range paths {
		if filepath.ToSlash(path) == parentBehaviorEvalPath || isGenericQualityEvidencePath(path) {
			return true
		}
	}
	return false
}

func verifyQualityEvidenceBaseline(repoRoot, baselineHead string) error {
	command := exec.Command("git", "-C", repoRoot, "cat-file", "-e", baselineHead+"^{commit}")
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("quality evidence baseline: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func classifyRepositoryQualityEvidence(repoRoot, baselineHead, path string) (qualityEvidenceDecision, bool, error) {
	if filepath.ToSlash(path) != parentBehaviorEvalPath {
		return qualityEvidenceDecision{}, false, nil
	}
	before, beforeExists, err := readQualityEvidenceBaselineFile(repoRoot, baselineHead, path)
	if err != nil {
		return qualityEvidenceDecision{}, true, err
	}
	after, afterExists, err := readQualityEvidenceWorktreeFile(repoRoot, path)
	if err != nil {
		return qualityEvidenceDecision{}, true, err
	}
	decision, err := compareParentBehaviorEvalRegistry(before, beforeExists, after, afterExists)
	return decision, true, err
}

func compareParentBehaviorEvalRegistry(before []byte, beforeExists bool, after []byte, afterExists bool) (qualityEvidenceDecision, error) {
	if !beforeExists {
		return qualityEvidenceDecision{}, nil
	}
	if !afterExists {
		return qualityEvidenceDecision{High: true, Source: "track-b-live-eval-registry-removed", HitPath: parentBehaviorEvalPath}, nil
	}
	beforeCases, err := parseParentBehaviorEvalCases(before)
	if err != nil {
		return qualityEvidenceDecision{}, fmt.Errorf("baseline live eval registry: %w", err)
	}
	afterCases, err := parseParentBehaviorEvalCases(after)
	if err != nil {
		return qualityEvidenceDecision{}, fmt.Errorf("current live eval registry: %w", err)
	}
	for id, oldCase := range beforeCases {
		newCase, ok := afterCases[id]
		if !ok || !sameParentBehaviorEvalContract(oldCase, newCase) {
			return qualityEvidenceDecision{High: true, Source: "track-b-live-eval-contract-changed", HitPath: id}, nil
		}
	}
	return qualityEvidenceDecision{}, nil
}

func parseParentBehaviorEvalCases(data []byte) (map[string]parentBehaviorEvalCase, error) {
	var registry parentBehaviorEvalRegistry
	if err := json.Unmarshal(data, &registry); err != nil {
		return nil, err
	}
	cases := make(map[string]parentBehaviorEvalCase, len(registry.Cases))
	for _, item := range registry.Cases {
		if strings.TrimSpace(item.ID) == "" {
			return nil, fmt.Errorf("live eval case without id")
		}
		if _, exists := cases[item.ID]; exists {
			return nil, fmt.Errorf("duplicate live eval case: %s", item.ID)
		}
		item.ContractSources = append([]string(nil), item.ContractSources...)
		sort.Strings(item.ContractSources)
		cases[item.ID] = item
	}
	return cases, nil
}

func sameParentBehaviorEvalContract(a, b parentBehaviorEvalCase) bool {
	if a.Positive != b.Positive || a.Negative != b.Negative || a.Evidence != b.Evidence || a.RunPolicy != b.RunPolicy {
		return false
	}
	if len(a.ContractSources) != len(b.ContractSources) {
		return false
	}
	for i := range a.ContractSources {
		if a.ContractSources[i] != b.ContractSources[i] {
			return false
		}
	}
	return true
}

func isGenericQualityEvidencePath(path string) bool {
	critical, category := IsCriticalPath(path)
	if critical {
		return false
	}
	switch category {
	case testPathCategory, "test-fixture", "test-harness":
		return true
	default:
		return false
	}
}

func collectQualityEvidenceSignatures(repoRoot, baselineHead, path string, before, after map[string]int) error {
	oldData, oldExists, err := readQualityEvidenceBaselineFile(repoRoot, baselineHead, path)
	if err != nil {
		return err
	}
	newData, newExists, err := readQualityEvidenceWorktreeFile(repoRoot, path)
	if err != nil {
		return err
	}
	oldSignatures, err := qualityEvidenceSignatures(path, oldData, oldExists)
	if err != nil {
		return fmt.Errorf("baseline quality evidence %s: %w", path, err)
	}
	newSignatures, err := qualityEvidenceSignatures(path, newData, newExists)
	if err != nil {
		return fmt.Errorf("current quality evidence %s: %w", path, err)
	}
	addEvidenceSignatures(before, oldSignatures)
	addEvidenceSignatures(after, newSignatures)
	return nil
}

func readQualityEvidenceBaselineFile(repoRoot, baselineHead, path string) ([]byte, bool, error) {
	object := baselineHead + ":" + filepath.ToSlash(path)
	check := exec.Command("git", "-C", repoRoot, "cat-file", "-e", object)
	if err := check.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil, false, nil
		}
		return nil, false, err
	}
	data, err := exec.Command("git", "-C", repoRoot, "show", object).Output()
	if err != nil {
		return nil, false, err
	}
	return data, true, nil
}

func readQualityEvidenceWorktreeFile(repoRoot, path string) ([]byte, bool, error) {
	data, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(path)))
	if err == nil {
		return data, true, nil
	}
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	return nil, false, err
}

func qualityEvidenceSignatures(path string, data []byte, exists bool) ([]string, error) {
	if !exists {
		return nil, nil
	}
	lower := strings.ToLower(path)
	if strings.HasSuffix(lower, ".go") {
		return goQualityEvidenceSignatures(data)
	}
	if strings.HasSuffix(lower, ".json") {
		return jsonQualityEvidenceSignatures(data)
	}
	return textQualityEvidenceSignatures(data), nil
}
func goQualityEvidenceSignatures(data []byte) ([]string, error) {
	file, err := parser.ParseFile(token.NewFileSet(), "quality_evidence_test.go", data, 0)
	if err != nil {
		return nil, err
	}
	signatures := []string{"file:go-test"}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil || !isGoTestEntryName(fn.Name.Name) {
			continue
		}
		signatures = append(signatures, "test-entry")
		signatures = append(signatures, goTestBodyEvidence(fn.Body)...)
	}
	return signatures, nil
}

func isGoTestEntryName(name string) bool {
	for _, prefix := range []string{"Test", "Benchmark", "Fuzz", "Example"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func goTestBodyEvidence(body *ast.BlockStmt) []string {
	var signatures []string
	ast.Inspect(body, func(node ast.Node) bool {
		switch value := node.(type) {
		case *ast.CallExpr:
			if isGoFailureCall(value) {
				break
			}
			if selector, ok := value.Fun.(*ast.SelectorExpr); ok && selector.Sel.Name == "Run" {
				signatures = append(signatures, "subtest:"+goSubtestEvidence(value))
				break
			}
			signatures = append(signatures, "call:"+normalizeGoEvidenceNode(value))
		case *ast.IfStmt:
			if goNodeContainsFailure(value.Body) || goNodeContainsFailure(value.Else) {
				signatures = append(signatures, "failure-guard:"+normalizeGoEvidenceNode(value.Cond))
			}
		}
		return true
	})
	return signatures
}

func goSubtestEvidence(call *ast.CallExpr) string {
	if len(call.Args) == 0 {
		return "<missing-name>"
	}
	return normalizeGoEvidenceNode(call.Args[0])
}

func goNodeContainsFailure(node ast.Node) bool {
	if node == nil {
		return false
	}
	found := false
	ast.Inspect(node, func(current ast.Node) bool {
		call, ok := current.(*ast.CallExpr)
		if ok && isGoFailureCall(call) {
			found = true
			return false
		}
		return !found
	})
	return found
}

func isGoFailureCall(call *ast.CallExpr) bool {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	switch selector.Sel.Name {
	case "Error", "Errorf", "Fatal", "Fatalf", "Fail", "FailNow":
		return true
	default:
		return false
	}
}

func normalizeGoEvidenceNode(node any) string {
	var formatted bytes.Buffer
	if err := format.Node(&formatted, token.NewFileSet(), node); err != nil {
		return "<format-error>"
	}
	var lexical scanner.Scanner
	files := token.NewFileSet()
	file := files.AddFile("evidence.go", -1, formatted.Len())
	lexical.Init(file, formatted.Bytes(), nil, 0)
	var result strings.Builder
	previous := token.ILLEGAL
	for {
		_, tok, literal := lexical.Scan()
		if tok == token.EOF {
			break
		}
		piece := literal
		if piece == "" {
			piece = tok.String()
		}
		if tok == token.IDENT && previous != token.PERIOD && piece != "true" && piece != "false" && piece != "nil" {
			piece = "_"
		}
		result.WriteString(piece)
		previous = tok
	}
	return result.String()
}

func jsonQualityEvidenceSignatures(data []byte) ([]string, error) {
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, err
	}
	signatures := []string{"file:json-test"}
	for range countJSONEvidenceUnits(value) {
		signatures = append(signatures, "json-unit")
	}
	return signatures, nil
}

func countJSONEvidenceUnits(value any) int {
	switch item := value.(type) {
	case map[string]any:
		count := 0
		for _, child := range item {
			count++
			count += countJSONEvidenceUnits(child)
		}
		return count
	case []any:
		count := 0
		for _, child := range item {
			count++
			count += countJSONEvidenceUnits(child)
		}
		return count
	default:
		return 1
	}
}

func textQualityEvidenceSignatures(data []byte) []string {
	signatures := []string{"file:text-test"}
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.Join(strings.Fields(raw), " ")
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}
		signatures = append(signatures, "text-unit")
	}
	return signatures
}
func addEvidenceSignatures(target map[string]int, values []string) {
	for _, value := range values {
		target[value]++
	}
}

func firstMissingEvidenceSignature(before, after map[string]int) string {
	keys := make([]string, 0, len(before))
	for key := range before {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if before[key] > after[key] {
			return key
		}
	}
	return ""
}
