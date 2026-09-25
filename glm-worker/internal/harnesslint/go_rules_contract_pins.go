package harnesslint

import (
	"go/ast"
	"go/token"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

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
		if hasDocumentReference(segment) {
			violations = append(violations, prosePinFunctionViolations(set, path, function)...)
		}
	}
	return violations
}

func prosePinFunctionViolations(set *token.FileSet, path string, function *ast.FuncDecl) []Violation {
	var violations []Violation
	ast.Inspect(function.Body, func(node ast.Node) bool {
		violations = append(violations, prosePinNodeViolations(set, path, node)...)
		return true
	})
	return violations
}

func prosePinNodeViolations(set *token.FileSet, path string, node ast.Node) []Violation {
	switch typed := node.(type) {
	case *ast.CallExpr:
		if !isStringPinCall(typed.Fun) {
			return nil
		}
		return proseLiteralViolations(set, path, typed.Args, "test must not pin long natural-language instruction or Markdown prose")
	case *ast.BinaryExpr:
		if typed.Op != token.EQL && typed.Op != token.NEQ {
			return nil
		}
		return proseLiteralViolations(set, path, []ast.Expr{typed.X, typed.Y}, "test must not exact-pin long natural-language instruction or Markdown prose")
	default:
		return nil
	}
}

func proseLiteralViolations(set *token.FileSet, path string, expressions []ast.Expr, message string) []Violation {
	var violations []Violation
	for _, expression := range expressions {
		value, ok := stringLiteral(expression)
		if !ok || !proseLike(value) {
			continue
		}
		position := set.Position(expression.Pos())
		violations = append(violations, Violation{
			Rule: "prose-contract-pin", Path: path, Line: position.Line, Column: position.Column,
			Message: message,
		})
	}
	return violations
}

func instructionHashGoViolations(set *token.FileSet, file *ast.File, path string, data []byte) []Violation {
	if !strings.HasSuffix(path, "_test.go") || !importsPackage(file, "crypto/sha256") {
		return nil
	}
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Body == nil || !isGoTestEntrypoint(function.Name.Name) {
			continue
		}
		segment := nodeText(data, set.Position(function.Pos()).Offset, set.Position(function.End()).Offset)
		if !hasDocumentReference(segment) || !containsSHA256Reference(segment) {
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

func importsPackage(file *ast.File, packagePath string) bool {
	for _, spec := range file.Imports {
		value, err := strconv.Unquote(spec.Path.Value)
		if err == nil && value == packagePath {
			return true
		}
	}
	return false
}

func containsSHA256Reference(segment string) bool {
	return strings.Contains(segment, "sha256") || strings.Contains(segment, "SHA256")
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
