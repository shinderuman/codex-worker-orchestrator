package harnesslint

import (
	"go/ast"
	"go/token"
	"strings"
)

func thinWrapperViolations(set *token.FileSet, file *ast.File, path string, data []byte) []Violation {
	if strings.HasSuffix(path, "_test.go") || hasBuildConstraint(data) {
		return nil
	}
	var violations []Violation
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if violation, ok := thinWrapperViolation(set, path, function); ok {
			violations = append(violations, violation)
		}
	}
	return violations
}

func thinWrapperViolation(set *token.FileSet, path string, function *ast.FuncDecl) (Violation, bool) {
	if function.Recv != nil || function.Body == nil || function.Name.IsExported() || function.Name.Name == mainFunctionName {
		return Violation{}, false
	}
	parameters := parameterNames(function.Type.Params)
	if len(parameters) == 0 || len(function.Body.List) != 1 {
		return Violation{}, false
	}
	call := forwardedCall(function.Body.List[0])
	if call == nil || !argumentsForward(parameters, call.Args) {
		return Violation{}, false
	}
	position := set.Position(function.Pos())
	return Violation{
		Rule: "thin-wrapper-proliferation", Path: path, Line: position.Line, Column: position.Column,
		Message: "private forwarding wrapper adds no validation, transformation, or ownership boundary",
	}, true
}

func hasBuildConstraint(data []byte) bool {
	prefix := string(data)
	if len(prefix) > 1024 {
		prefix = prefix[:1024]
	}
	return strings.Contains(prefix, "//go:build") || strings.Contains(prefix, "// +build")
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
