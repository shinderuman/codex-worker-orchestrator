package harnesslint

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
)

const (
	cleanCutoverRule             = "clean-cutover-rejection-only-surface"
	parentActionMetadataPath     = "glm-worker/internal/parentactioncmd/command_metadata.go"
	parentActionRegistryName     = "parentActionCommands"
	parentActionLegacyPredicate  = "isLegacyParentActionInvocation"
	parentActionExecutionParam   = "execution"
	parentActionActionParam      = "action"
)

func scanCleanCutover(root string, paths []string) ([]Violation, error) {
	if !containsPath(paths, parentActionMetadataPath) {
		return nil, nil
	}
	return cleanCutoverParentActionViolations(root)
}

func containsPath(paths []string, wanted string) bool {
	for _, path := range paths {
		if path == wanted {
			return true
		}
	}
	return false
}

func cleanCutoverParentActionViolations(root string) ([]Violation, error) {
	data, err := readRegularFile(root, parentActionMetadataPath)
	if err != nil {
		return nil, err
	}
	set := token.NewFileSet()
	file, err := parser.ParseFile(set, parentActionMetadataPath, data, 0)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", parentActionMetadataPath, err)
	}
	rejectedKinds, rejectedActions := parentActionRejectedRoots(file)
	if len(rejectedKinds) == 0 && len(rejectedActions) == 0 {
		return nil, nil
	}
	return parentActionRegistryViolations(set, file, rejectedKinds, rejectedActions), nil
}

func parentActionRejectedRoots(file *ast.File) (map[string]bool, map[string]bool) {
	kinds := make(map[string]bool)
	actions := make(map[string]bool)
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Name.Name != parentActionLegacyPredicate || function.Body == nil {
			continue
		}
		ast.Inspect(function.Body, func(node ast.Node) bool {
			switch typed := node.(type) {
			case *ast.SwitchStmt:
				identifier, ok := typed.Tag.(*ast.Ident)
				if !ok || identifier.Name != parentActionExecutionParam {
					return true
				}
				for _, statement := range typed.Body.List {
					clause, ok := statement.(*ast.CaseClause)
					if !ok || !returnsBoolean(clause.Body, true) {
						continue
					}
					for _, expression := range clause.List {
						if identifier, ok := expression.(*ast.Ident); ok {
							kinds[identifier.Name] = true
						}
					}
				}
			case *ast.ReturnStmt:
				for _, result := range typed.Results {
					if action, ok := parentActionRejectedAction(result); ok {
						actions[action] = true
					}
				}
			}
			return true
		})
	}
	return kinds, actions
}

func returnsBoolean(statements []ast.Stmt, wanted bool) bool {
	for _, statement := range statements {
		result, ok := statement.(*ast.ReturnStmt)
		if !ok || len(result.Results) != 1 {
			continue
		}
		identifier, ok := result.Results[0].(*ast.Ident)
		if ok && identifier.Name == strconv.FormatBool(wanted) {
			return true
		}
	}
	return false
}

func parentActionRejectedAction(expression ast.Expr) (string, bool) {
	comparison, ok := expression.(*ast.BinaryExpr)
	if !ok || comparison.Op != token.EQL {
		return "", false
	}
	if identifier, ok := comparison.X.(*ast.Ident); ok && identifier.Name == parentActionActionParam {
		return stringLiteral(comparison.Y)
	}
	if identifier, ok := comparison.Y.(*ast.Ident); ok && identifier.Name == parentActionActionParam {
		return stringLiteral(comparison.X)
	}
	return "", false
}

func stringLiteral(expression ast.Expr) (string, bool) {
	literal, ok := expression.(*ast.BasicLit)
	if !ok || literal.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(literal.Value)
	if err != nil {
		return "", false
	}
	return value, true
}

func parentActionRegistryViolations(set *token.FileSet, file *ast.File, rejectedKinds, rejectedActions map[string]bool) []Violation {
	var violations []Violation
	for _, declaration := range file.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok || general.Tok != token.VAR {
			continue
		}
		for _, specification := range general.Specs {
			value, ok := specification.(*ast.ValueSpec)
			if !ok || len(value.Names) != 1 || value.Names[0].Name != parentActionRegistryName || len(value.Values) != 1 {
				continue
			}
			registry, ok := value.Values[0].(*ast.CompositeLit)
			if !ok {
				continue
			}
			for _, element := range registry.Elts {
				entry, ok := element.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				if parentActionRegistryEntryRejected(entry, rejectedKinds, rejectedActions) {
					position := set.Position(entry.Key.Pos())
					violations = append(violations, Violation{
						Rule: cleanCutoverRule, Path: parentActionMetadataPath, Line: position.Line, Column: position.Column,
						Message: "registered parent action is retained solely behind an unconditional cutover rejection; remove the registry root",
					})
				}
			}
		}
	}
	return violations
}

func parentActionRegistryEntryRejected(entry *ast.KeyValueExpr, rejectedKinds, rejectedActions map[string]bool) bool {
	if action, ok := stringLiteral(entry.Key); ok && rejectedActions[action] {
		return true
	}
	descriptor, ok := entry.Value.(*ast.CompositeLit)
	if !ok {
		return false
	}
	for _, element := range descriptor.Elts {
		field, ok := element.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		name, ok := field.Key.(*ast.Ident)
		if !ok || (name.Name != "Execute" && name.Name != "TerminalExecute") {
			continue
		}
		kind, ok := field.Value.(*ast.Ident)
		if ok && rejectedKinds[kind.Name] {
			return true
		}
	}
	return false
}
