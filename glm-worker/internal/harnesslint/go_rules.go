package harnesslint

import (
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

func scanGoRules(root string, paths []string) ([]Violation, error) {
	var violations []Violation
	for _, path := range goFiles(paths) {
		data, err := readRegularFile(root, path)
		if err != nil {
			return nil, err
		}
		if formatted, formatErr := format.Source(data); formatErr == nil && string(formatted) != string(data) {
			violations = append(violations, Violation{
				Rule: "gofmt", Path: path, Line: 1, Column: 1,
				Message: "Go source differs from gofmt output", Fixable: true,
			})
		}
		set := token.NewFileSet()
		file, err := parser.ParseFile(set, path, data, 0)
		if err != nil {
			violations = append(violations, Violation{Rule: "go-parse", Path: path, Line: 1, Column: 1, Message: err.Error()})
			continue
		}
		violations = append(violations, entrypointViolations(set, file, path)...)
		violations = append(violations, prosePinGoViolations(set, file, path, data)...)
		violations = append(violations, instructionHashGoViolations(set, file, path, data)...)
		violations = append(violations, shadowProductionViolations(set, file, path)...)
		violations = append(violations, scenarioSelfTestViolations(set, file, path, data)...)
		violations = append(violations, testSizeViolations(set, file, path)...)
		violations = append(violations, thinWrapperViolations(set, file, path, data)...)
	}
	return violations, nil
}

func entrypointViolations(set *token.FileSet, file *ast.File, path string) []Violation {
	if filepath.Base(path) != "main.go" || file.Name.Name != "main" || !isCommandMain(path) {
		return nil
	}
	var violations []Violation
	var mainDecl *ast.FuncDecl
	for _, declaration := range file.Decls {
		switch typed := declaration.(type) {
		case *ast.FuncDecl:
			if typed.Name.Name == "main" {
				mainDecl = typed
				continue
			}
			position := set.Position(typed.Pos())
			violations = append(violations, Violation{
				Rule: "entrypoint-thin", Path: path, Line: position.Line, Column: position.Column,
				Message: "cmd main.go must not contain helper or business functions",
			})
		case *ast.GenDecl:
			if typed.Tok == token.IMPORT {
				continue
			}
			position := set.Position(typed.Pos())
			violations = append(violations, Violation{
				Rule: "entrypoint-thin", Path: path, Line: position.Line, Column: position.Column,
				Message: "cmd main.go must not contain type, const, or var declarations",
			})
		}
	}
	if mainDecl == nil {
		violations = append(violations, Violation{Rule: "entrypoint-thin", Path: path, Line: 1, Column: 1, Message: "cmd main.go must contain main"})
		return violations
	}
	if statementCount(mainDecl.Body) > 8 {
		position := set.Position(mainDecl.Pos())
		violations = append(violations, Violation{
			Rule: "entrypoint-thin", Path: path, Line: position.Line, Column: position.Column,
			Message: "main must only delegate to an internal command and handle its terminal error",
		})
	}
	return violations
}

func isCommandMain(path string) bool {
	normalized := "/" + filepath.ToSlash(path)
	return strings.Contains(normalized, "/cmd/")
}

func prosePinGoViolations(set *token.FileSet, file *ast.File, path string, data []byte) []Violation {
	if !strings.HasSuffix(path, "_test.go") {
		return nil
	}
	var violations []Violation
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Body == nil || !isGoTestEntrypoint(function.Name.Name) {
			continue
		}
		segment := nodeText(data, set.Position(function.Pos()).Offset, set.Position(function.End()).Offset)
		if !hasDocumentReference(segment) {
			continue
		}
		ast.Inspect(function.Body, func(node ast.Node) bool {
			switch typed := node.(type) {
			case *ast.CallExpr:
				if !isStringPinCall(typed.Fun) {
					return true
				}
				for _, argument := range typed.Args {
					value, ok := stringLiteral(argument)
					if !ok || !proseLike(value) {
						continue
					}
					position := set.Position(argument.Pos())
					violations = append(violations, Violation{
						Rule: "prose-contract-pin", Path: path, Line: position.Line, Column: position.Column,
						Message: "test must not pin long natural-language instruction or Markdown prose",
					})
				}
			case *ast.BinaryExpr:
				if typed.Op != token.EQL && typed.Op != token.NEQ {
					return true
				}
				for _, expression := range []ast.Expr{typed.X, typed.Y} {
					value, ok := stringLiteral(expression)
					if !ok || !proseLike(value) {
						continue
					}
					position := set.Position(expression.Pos())
					violations = append(violations, Violation{
						Rule: "prose-contract-pin", Path: path, Line: position.Line, Column: position.Column,
						Message: "test must not exact-pin long natural-language instruction or Markdown prose",
					})
				}
			}
			return true
		})
	}
	return violations
}

func instructionHashGoViolations(set *token.FileSet, file *ast.File, path string, data []byte) []Violation {
	if !strings.HasSuffix(path, "_test.go") {
		return nil
	}
	importsSHA256 := false
	for _, spec := range file.Imports {
		value, err := strconv.Unquote(spec.Path.Value)
		if err == nil && value == "crypto/sha256" {
			importsSHA256 = true
			break
		}
	}
	if !importsSHA256 {
		return nil
	}
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Body == nil || !isGoTestEntrypoint(function.Name.Name) {
			continue
		}
		segment := nodeText(data, set.Position(function.Pos()).Offset, set.Position(function.End()).Offset)
		if !hasDocumentReference(segment) || (!strings.Contains(segment, "sha256") && !strings.Contains(segment, "SHA256")) {
			continue
		}
		position := set.Position(function.Pos())
		return []Violation{{
			Rule: "instruction-content-hash", Path: path, Line: position.Line, Column: position.Column,
			Message: "tests must not make whole instruction or Markdown file hashes a contract",
		}}
	}
	return nil
}

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

func thinWrapperViolations(set *token.FileSet, file *ast.File, path string, data []byte) []Violation {
	if strings.HasSuffix(path, "_test.go") || hasBuildConstraint(data) {
		return nil
	}
	var violations []Violation
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Recv != nil || function.Body == nil || function.Name.IsExported() || function.Name.Name == "main" {
			continue
		}
		parameters := parameterNames(function.Type.Params)
		if len(parameters) == 0 || len(function.Body.List) != 1 {
			continue
		}
		call := forwardedCall(function.Body.List[0])
		if call == nil || !argumentsForward(parameters, call.Args) {
			continue
		}
		position := set.Position(function.Pos())
		violations = append(violations, Violation{
			Rule: "thin-wrapper-proliferation", Path: path, Line: position.Line, Column: position.Column,
			Message: "private forwarding wrapper adds no validation, transformation, or ownership boundary",
		})
	}
	return violations
}

func hasBuildConstraint(data []byte) bool {
	prefix := string(data)
	if len(prefix) > 1024 {
		prefix = prefix[:1024]
	}
	return strings.Contains(prefix, "//go:build") || strings.Contains(prefix, "// +build")
}

func statementCount(node ast.Node) int {
	count := 0
	ast.Inspect(node, func(current ast.Node) bool {
		if _, ok := current.(ast.Stmt); ok {
			count++
		}
		return true
	})
	return count
}

func branchCount(node ast.Node) int {
	count := 0
	ast.Inspect(node, func(current ast.Node) bool {
		switch current.(type) {
		case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt, *ast.SwitchStmt, *ast.TypeSwitchStmt, *ast.SelectStmt:
			count++
		case *ast.CaseClause:
			count++
		}
		return true
	})
	return count
}

func isGoTestEntrypoint(name string) bool {
	return strings.HasPrefix(name, "Test") || strings.HasPrefix(name, "Benchmark") ||
		strings.HasPrefix(name, "Fuzz") || strings.HasPrefix(name, "Example")
}

func parameterNames(fields *ast.FieldList) []string {
	if fields == nil {
		return nil
	}
	var names []string
	for _, field := range fields.List {
		if len(field.Names) == 0 {
			return nil
		}
		for _, name := range field.Names {
			names = append(names, name.Name)
		}
	}
	return names
}

func forwardedCall(statement ast.Stmt) *ast.CallExpr {
	switch typed := statement.(type) {
	case *ast.ReturnStmt:
		if len(typed.Results) != 1 {
			return nil
		}
		call, _ := typed.Results[0].(*ast.CallExpr)
		return call
	case *ast.ExprStmt:
		call, _ := typed.X.(*ast.CallExpr)
		return call
	default:
		return nil
	}
}

func argumentsForward(parameters []string, arguments []ast.Expr) bool {
	if len(parameters) != len(arguments) {
		return false
	}
	for index, argument := range arguments {
		identifier, ok := argument.(*ast.Ident)
		if !ok || identifier.Name != parameters[index] {
			return false
		}
	}
	return true
}

func isStringPinCall(expression ast.Expr) bool {
	selector, ok := expression.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := selector.X.(*ast.Ident)
	if !ok || (pkg.Name != "strings" && pkg.Name != "bytes") {
		return false
	}
	switch selector.Sel.Name {
	case "Contains", "EqualFold", "HasPrefix", "HasSuffix":
		return true
	default:
		return false
	}
}

func stringLiteral(expression ast.Expr) (string, bool) {
	literal, ok := expression.(*ast.BasicLit)
	if !ok || literal.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(literal.Value)
	return value, err == nil
}

func hasDocumentReference(text string) bool {
	return strings.Contains(text, ".md") || strings.Contains(text, "codex/instructions/") ||
		strings.Contains(text, "codex/glm-worker/prompts/") || strings.Contains(text, "AGENTS")
}

func proseLike(value string) bool {
	if utf8.RuneCountInString(value) < 32 {
		return false
	}
	words := len(strings.Fields(value))
	hasJapanese := false
	for _, value := range value {
		if unicode.In(value, unicode.Hiragana, unicode.Katakana, unicode.Han) {
			hasJapanese = true
			break
		}
	}
	return hasJapanese || words >= 5
}

func nodeText(data []byte, start, end int) string {
	if start < 0 || end < start || start > len(data) {
		return ""
	}
	if end > len(data) {
		end = len(data)
	}
	return string(data[start:end])
}
