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

func emptyTestEntrypointViolations(set *token.FileSet, file *ast.File, path string) []Violation {
	if !strings.HasSuffix(path, "_test.go") {
		return nil
	}
	var violations []Violation
	for _, decl := range file.Decls {
		function, ok := decl.(*ast.FuncDecl)
		if !ok || !isGoTestEntrypoint(function.Name.Name) || function.Body == nil || !inertGoTestEntrypoint(function) {
			continue
		}
		position := set.Position(function.Pos())
		violations = append(violations, Violation{
			Rule: "empty-test-entrypoint", Path: path, Line: position.Line, Column: position.Column,
			Message: "empty test entrypoint does not observe production behavior or establish coverage",
		})
	}
	return violations
}

func inertGoTestEntrypoint(function *ast.FuncDecl) bool {
	if len(function.Body.List) == 0 {
		return true
	}
	if function.Type.Params == nil || len(function.Type.Params.List) != 1 || len(function.Type.Params.List[0].Names) != 1 {
		return false
	}
	receiver := function.Type.Params.List[0].Names[0].Name
	for _, statement := range function.Body.List {
		if !inertTestSetupStatement(statement, receiver) {
			return false
		}
	}
	return true
}

func inertTestSetupStatement(statement ast.Stmt, receiver string) bool {
	expression, ok := statement.(*ast.ExprStmt)
	if !ok {
		return false
	}
	call, ok := expression.X.(*ast.CallExpr)
	if !ok || len(call.Args) != 0 {
		return false
	}
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || (selector.Sel.Name != "Parallel" && selector.Sel.Name != "Helper") {
		return false
	}
	base, ok := selector.X.(*ast.Ident)
	return ok && base.Name == receiver
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
