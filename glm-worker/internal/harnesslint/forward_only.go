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
	ast.Inspect(file, func(node ast.Node) bool {
		statement, ok := node.(*ast.IfStmt)
		if !ok {
			return true
		}
		target, oldValue, ok := versionEqualityTarget(statement.Cond)
		if !ok {
			return true
		}
		ast.Inspect(statement.Body, func(bodyNode ast.Node) bool {
			assignment, ok := bodyNode.(*ast.AssignStmt)
			if !ok || assignment.Tok != token.ASSIGN || len(assignment.Lhs) != 1 || len(assignment.Rhs) != 1 {
				return true
			}
			if !sameGoExpr(set, assignment.Lhs[0], target) || sameGoExpr(set, assignment.Rhs[0], target) || sameGoExpr(set, assignment.Rhs[0], oldValue) {
				return true
			}
			if !versionConstantExpr(assignment.Rhs[0]) {
				return true
			}
			position := set.Position(assignment.Pos())
			violations = append(violations, Violation{
				Rule: forwardOnlyCompatibilityRule,
				Path: path,
				Line: position.Line,
				Column: position.Column,
				Message: "old version/revision must not be promoted into the current machine contract",
			})
			return true
		})
		return true
	})
	return violations
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
