package harnesslint

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"strings"
)

const forwardOnlyCompatibilityRule = "forward-only-compatibility"

func scanForwardOnlyRules(root string, paths []string) ([]Violation, error) {
	var violations []Violation
	for _, path := range goFiles(paths) {
		data, err := readRegularFile(root, path)
		if err != nil {
			return nil, err
		}
		set := token.NewFileSet()
		file, err := parser.ParseFile(set, path, data, 0)
		if err != nil {
			continue
		}
		violations = append(violations, forwardOnlyGoViolations(set, file, path)...)
	}
	return violations, nil
}

func forwardOnlyGoViolations(set *token.FileSet, file *ast.File, path string) []Violation {
	var violations []Violation
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Body == nil || !forwardOnlyReaderFunction(function.Name.Name) {
			continue
		}
		violations = append(violations, forwardOnlyFunctionViolations(set, function, path)...)
	}
	return violations
}

func forwardOnlyFunctionViolations(set *token.FileSet, function *ast.FuncDecl, path string) []Violation {
	var violations []Violation
	ast.Inspect(function.Body, func(node ast.Node) bool {
		statement, ok := node.(*ast.IfStmt)
		if !ok {
			return true
		}
		target, oldValue, ok := versionEqualityTarget(statement.Cond)
		if !ok {
			return true
		}
		violations = append(violations, versionPromotionViolations(set, statement.Body, path, target, oldValue)...)
		return true
	})
	return violations
}

func versionPromotionViolations(set *token.FileSet, body *ast.BlockStmt, path string, target, oldValue ast.Expr) []Violation {
	var violations []Violation
	ast.Inspect(body, func(node ast.Node) bool {
		assignment, ok := node.(*ast.AssignStmt)
		if !ok || !versionPromotionAssignment(set, assignment, target, oldValue) {
			return true
		}
		position := set.Position(assignment.Pos())
		violations = append(violations, Violation{
			Rule:    forwardOnlyCompatibilityRule,
			Path:    path,
			Line:    position.Line,
			Column:  position.Column,
			Message: "old version/revision must not be promoted into the current machine contract",
		})
		return true
	})
	return violations
}

func versionPromotionAssignment(set *token.FileSet, assignment *ast.AssignStmt, target, oldValue ast.Expr) bool {
	if assignment.Tok != token.ASSIGN || len(assignment.Lhs) != 1 || len(assignment.Rhs) != 1 {
		return false
	}
	if !sameGoExpr(set, assignment.Lhs[0], target) || sameGoExpr(set, assignment.Rhs[0], target) || sameGoExpr(set, assignment.Rhs[0], oldValue) {
		return false
	}
	return versionConstantExpr(assignment.Rhs[0])
}

func forwardOnlyReaderFunction(name string) bool {
	lower := strings.ToLower(name)
	for _, fragment := range []string{"decode", "load", "read", "parse", "unmarshal"} {
		if strings.Contains(lower, fragment) {
			return true
		}
	}
	return false
}

func versionEqualityTarget(expr ast.Expr) (ast.Expr, ast.Expr, bool) {
	binary, ok := expr.(*ast.BinaryExpr)
	if !ok || binary.Op != token.EQL {
		return nil, nil, false
	}
	if versionFieldExpr(binary.X) {
		return binary.X, binary.Y, true
	}
	if versionFieldExpr(binary.Y) {
		return binary.Y, binary.X, true
	}
	return nil, nil, false
}

func versionFieldExpr(expr ast.Expr) bool {
	name := expressionTerminalName(expr)
	return name == "version" || strings.Contains(name, "schemarevision") || name == "revision"
}

func versionConstantExpr(expr ast.Expr) bool {
	name := expressionTerminalName(expr)
	return name != "" && (strings.Contains(name, "version") || strings.Contains(name, "revision"))
}

func expressionTerminalName(expr ast.Expr) string {
	switch typed := expr.(type) {
	case *ast.Ident:
		return strings.ToLower(typed.Name)
	case *ast.SelectorExpr:
		return strings.ToLower(typed.Sel.Name)
	default:
		return ""
	}
}

func sameGoExpr(set *token.FileSet, left, right ast.Expr) bool {
	return goExprText(set, left) == goExprText(set, right)
}

func goExprText(set *token.FileSet, expr ast.Expr) string {
	var buffer bytes.Buffer
	if err := format.Node(&buffer, set, expr); err != nil {
		return ""
	}
	return buffer.String()
}
