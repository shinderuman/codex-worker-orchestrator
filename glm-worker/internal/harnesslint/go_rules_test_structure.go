package harnesslint

import (
	"go/ast"
	"go/token"
	"strings"
)

func shadowProductionViolations(set *token.FileSet, file *ast.File, path string) []Violation {
	if !strings.HasSuffix(path, "_test.go") {
		return nil
	}
	prefixes := []string{"orchestrate", "simulate", "applyUpdate", "updateAndVerify", "verificationOutcome", "fetchWakeReset", "wakeFailure"}
	var violations []Violation
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Body == nil || isGoTestEntrypoint(function.Name.Name) {
			continue
		}
		matched := false
		for _, prefix := range prefixes {
			if strings.HasPrefix(function.Name.Name, prefix) {
				matched = true
				break
			}
		}
		if !matched || branchCount(function.Body) < 3 {
			continue
		}
		position := set.Position(function.Pos())
		violations = append(violations, Violation{
			Rule: "test-shadow-production", Path: path, Line: position.Line, Column: position.Column,
			Message: "test helper reimplements orchestration or state-machine behavior instead of driving production",
		})
	}
	return violations
}

func scenarioSelfTestViolations(set *token.FileSet, file *ast.File, path string, data []byte) []Violation {
	if !strings.HasSuffix(path, "_test.go") || strings.Contains(path, "/internal/harnesslint/") || !strings.Contains(string(data), "scenarios") {
		return nil
	}
	var violations []Violation
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || !strings.HasPrefix(function.Name.Name, "Test") || function.Body == nil {
			continue
		}
		segment := nodeText(data, set.Position(function.Pos()).Offset, set.Position(function.End()).Offset)
		lower := strings.ToLower(segment)
		if !strings.Contains(function.Name.Name, "CorpusContract") &&
			!strings.Contains(lower, "scenariocount") &&
			!strings.Contains(lower, "requiredscenario") &&
			!strings.Contains(lower, "manifest.") {
			continue
		}
		position := set.Position(function.Pos())
		violations = append(violations, Violation{
			Rule: "scenario-self-test", Path: path, Line: position.Line, Column: position.Column,
			Message: "scenario corpus must drive production behavior, not maintain a second self-test contract",
		})
	}
	return violations
}

func testSizeViolations(set *token.FileSet, file *ast.File, path string) []Violation {
	if !strings.HasSuffix(path, "_test.go") {
		return nil
	}
	var violations []Violation
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Body == nil || !isGoTestEntrypoint(function.Name.Name) {
			continue
		}
		lines := set.Position(function.End()).Line - set.Position(function.Pos()).Line + 1
		statements := statementCount(function.Body)
		if lines <= 150 && statements <= 100 {
			continue
		}
		position := set.Position(function.Pos())
		violations = append(violations, Violation{
			Rule: "test-size-limit", Path: path, Line: position.Line, Column: position.Column,
			Message: "test entrypoint exceeds 150 lines or 100 statements; split by behavior instead of hiding assertions in helpers",
		})
	}
	return violations
}
