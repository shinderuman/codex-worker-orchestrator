package harnesslint

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"strconv"
)

const (
	cleanCutoverRule         = "clean-cutover-rejection-only-surface"
	parentActionMetadataPath = "glm-worker/internal/parentactioncmd/command_metadata.go"
	parentActionRegistryName = "parentActionCommands"
	parentActionDispatchName = "executeParentActionCommand"
)

func scanCleanCutover(root string, paths []string) ([]Violation, error) {
	if !cleanCutoverContainsPath(paths, parentActionMetadataPath) {
		return nil, nil
	}
	return cleanCutoverParentActionViolations(root)
}

func fixCleanCutover(root string, paths []string) error {
	if !cleanCutoverContainsPath(paths, parentActionMetadataPath) {
		return nil
	}
	set, file, err := parseParentActionMetadata(root, parser.ParseComments)
	if err != nil {
		return err
	}
	rejectedKinds, rejectedActions := parentActionRejectedRoots(file)
	if !removeRejectedParentActionRegistryEntries(file, rejectedKinds, rejectedActions) {
		return nil
	}
	return writeFormattedParentActionMetadata(root, set, file)
}

func parseParentActionMetadata(root string, mode parser.Mode) (*token.FileSet, *ast.File, error) {
	data, err := readRegularFile(root, parentActionMetadataPath)
	if err != nil {
		return nil, nil, err
	}
	set := token.NewFileSet()
	file, err := parser.ParseFile(set, parentActionMetadataPath, data, mode)
	if err != nil {
		return nil, nil, fmt.Errorf("parse %s: %w", parentActionMetadataPath, err)
	}
	return set, file, nil
}

func removeRejectedParentActionRegistryEntries(file *ast.File, rejectedKinds, rejectedActions map[string]bool) bool {
	changed := false
	for _, registry := range parentActionRegistries(file) {
		kept := registry.Elts[:0]
		for _, element := range registry.Elts {
			entry, ok := element.(*ast.KeyValueExpr)
			if ok && parentActionRegistryEntryRejected(entry, rejectedKinds, rejectedActions) {
				changed = true
				continue
			}
			kept = append(kept, element)
		}
		registry.Elts = kept
	}
	return changed
}

func writeFormattedParentActionMetadata(root string, set *token.FileSet, file *ast.File) error {
	var output bytes.Buffer
	if err := format.Node(&output, set, file); err != nil {
		return fmt.Errorf("format %s: %w", parentActionMetadataPath, err)
	}
	formatted := output.Bytes()
	if len(formatted) == 0 || formatted[len(formatted)-1] != '\n' {
		formatted = append(formatted, '\n')
	}
	return writeRegularFile(root, parentActionMetadataPath, formatted)
}

func cleanCutoverContainsPath(paths []string, wanted string) bool {
	for _, path := range paths {
		if path == wanted {
			return true
		}
	}
	return false
}

func cleanCutoverParentActionViolations(root string) ([]Violation, error) {
	set, file, err := parseParentActionMetadata(root, 0)
	if err != nil {
		return nil, err
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
	predicate, executionParam, actionParam := findParentActionRejectionPredicate(file)
	if predicate == nil {
		return kinds, actions
	}
	collectRejectedParentActionKinds(predicate.Body, kinds, executionParam)
	collectRejectedParentActionNames(predicate.Body, actions, actionParam)
	return kinds, actions
}

func findParentActionRejectionPredicate(file *ast.File) (*ast.FuncDecl, string, string) {
	dispatch := findCleanCutoverFunction(file, parentActionDispatchName)
	if dispatch == nil || dispatch.Body == nil {
		return nil, "", ""
	}
	for _, statement := range dispatch.Body.List {
		conditional, ok := statement.(*ast.IfStmt)
		if !ok {
			continue
		}
		guardCall, executionIndex, actionIndex, ok := parentActionRejectionGuardCall(conditional)
		if !ok {
			continue
		}
		guardName, ok := cleanCutoverCalledFunctionName(guardCall)
		if !ok {
			continue
		}
		guard := findCleanCutoverFunction(file, guardName)
		guardParams := cleanCutoverParameterNames(guard)
		if guard == nil || executionIndex >= len(guardParams) || actionIndex >= len(guardParams) {
			continue
		}
		predicateCall, predicateExecutionIndex, predicateActionIndex, ok := cleanCutoverRejectionPredicateCall(
			guard,
			guardParams[executionIndex],
			guardParams[actionIndex],
		)
		if !ok {
			continue
		}
		predicateName, ok := cleanCutoverCalledFunctionName(predicateCall)
		if !ok {
			continue
		}
		predicate := findCleanCutoverFunction(file, predicateName)
		predicateParams := cleanCutoverParameterNames(predicate)
		if predicate == nil || predicateExecutionIndex >= len(predicateParams) || predicateActionIndex >= len(predicateParams) {
			continue
		}
		return predicate, predicateParams[predicateExecutionIndex], predicateParams[predicateActionIndex]
	}
	return nil, "", ""
}

func parentActionRejectionGuardCall(statement *ast.IfStmt) (*ast.CallExpr, int, int, bool) {
	assignment, ok := statement.Init.(*ast.AssignStmt)
	if !ok || len(assignment.Lhs) != 1 || len(assignment.Rhs) != 1 {
		return nil, 0, 0, false
	}
	errName, ok := assignment.Lhs[0].(*ast.Ident)
	if !ok || !cleanCutoverConditionIsNonNil(statement.Cond, errName.Name) || !cleanCutoverBlockReturnsIdentifier(statement.Body, errName.Name) {
		return nil, 0, 0, false
	}
	call, ok := assignment.Rhs[0].(*ast.CallExpr)
	if !ok || len(call.Args) != 2 {
		return nil, 0, 0, false
	}
	executionIndex := -1
	actionIndex := -1
	for index, argument := range call.Args {
		if _, ok := argument.(*ast.Ident); ok {
			executionIndex = index
			continue
		}
		selector, ok := argument.(*ast.SelectorExpr)
		if ok && selector.Sel.Name == "Action" {
			actionIndex = index
		}
	}
	if executionIndex < 0 || actionIndex < 0 || executionIndex == actionIndex {
		return nil, 0, 0, false
	}
	return call, executionIndex, actionIndex, true
}

func cleanCutoverConditionIsNonNil(expression ast.Expr, name string) bool {
	comparison, ok := expression.(*ast.BinaryExpr)
	if !ok || comparison.Op != token.NEQ {
		return false
	}
	return cleanCutoverIdentAndNil(comparison.X, comparison.Y, name) || cleanCutoverIdentAndNil(comparison.Y, comparison.X, name)
}

func cleanCutoverIdentAndNil(identifierExpr, nilExpr ast.Expr, name string) bool {
	identifier, ok := identifierExpr.(*ast.Ident)
	if !ok || identifier.Name != name {
		return false
	}
	nilIdentifier, ok := nilExpr.(*ast.Ident)
	return ok && nilIdentifier.Name == "nil"
}

func cleanCutoverBlockReturnsIdentifier(block *ast.BlockStmt, name string) bool {
	if block == nil {
		return false
	}
	for _, statement := range block.List {
		result, ok := statement.(*ast.ReturnStmt)
		if !ok || len(result.Results) != 1 {
			continue
		}
		identifier, ok := result.Results[0].(*ast.Ident)
		if ok && identifier.Name == name {
			return true
		}
	}
	return false
}

func cleanCutoverRejectionPredicateCall(guard *ast.FuncDecl, executionParam, actionParam string) (*ast.CallExpr, int, int, bool) {
	if guard == nil || guard.Body == nil {
		return nil, 0, 0, false
	}
	for index, statement := range guard.Body.List {
		conditional, ok := statement.(*ast.IfStmt)
		if !ok {
			continue
		}
		call, negated := cleanCutoverBooleanCall(conditional.Cond)
		if call == nil {
			continue
		}
		executionIndex, actionIndex, ok := cleanCutoverCallParameterIndexes(call, executionParam, actionParam)
		if !ok {
			continue
		}
		remaining := guard.Body.List[index+1:]
		if negated && cleanCutoverBlockReturnsNil(conditional.Body) && cleanCutoverStatementsReturnNonNil(remaining) {
			return call, executionIndex, actionIndex, true
		}
		if !negated && cleanCutoverBlockReturnsNonNil(conditional.Body) && cleanCutoverStatementsReturnNil(remaining) {
			return call, executionIndex, actionIndex, true
		}
	}
	return nil, 0, 0, false
}

func cleanCutoverBooleanCall(expression ast.Expr) (*ast.CallExpr, bool) {
	if call, ok := expression.(*ast.CallExpr); ok {
		return call, false
	}
	negation, ok := expression.(*ast.UnaryExpr)
	if !ok || negation.Op != token.NOT {
		return nil, false
	}
	call, ok := negation.X.(*ast.CallExpr)
	return call, ok
}

func cleanCutoverCallParameterIndexes(call *ast.CallExpr, executionParam, actionParam string) (int, int, bool) {
	if call == nil {
		return 0, 0, false
	}
	executionIndex := -1
	actionIndex := -1
	for index, argument := range call.Args {
		identifier, ok := argument.(*ast.Ident)
		if !ok {
			continue
		}
		switch identifier.Name {
		case executionParam:
			executionIndex = index
		case actionParam:
			actionIndex = index
		}
	}
	return executionIndex, actionIndex, executionIndex >= 0 && actionIndex >= 0 && executionIndex != actionIndex
}

func cleanCutoverBlockReturnsNil(block *ast.BlockStmt) bool {
	return block != nil && cleanCutoverStatementsReturnNil(block.List)
}

func cleanCutoverBlockReturnsNonNil(block *ast.BlockStmt) bool {
	return block != nil && cleanCutoverStatementsReturnNonNil(block.List)
}

func cleanCutoverStatementsReturnNil(statements []ast.Stmt) bool {
	for _, statement := range statements {
		result, ok := statement.(*ast.ReturnStmt)
		if !ok || len(result.Results) != 1 {
			continue
		}
		identifier, ok := result.Results[0].(*ast.Ident)
		if ok && identifier.Name == "nil" {
			return true
		}
	}
	return false
}

func cleanCutoverStatementsReturnNonNil(statements []ast.Stmt) bool {
	for _, statement := range statements {
		result, ok := statement.(*ast.ReturnStmt)
		if !ok || len(result.Results) != 1 {
			continue
		}
		identifier, isIdentifier := result.Results[0].(*ast.Ident)
		if !isIdentifier || identifier.Name != "nil" {
			return true
		}
	}
	return false
}

func cleanCutoverCalledFunctionName(call *ast.CallExpr) (string, bool) {
	if call == nil {
		return "", false
	}
	identifier, ok := call.Fun.(*ast.Ident)
	if !ok {
		return "", false
	}
	return identifier.Name, true
}

func findCleanCutoverFunction(file *ast.File, name string) *ast.FuncDecl {
	if file == nil {
		return nil
	}
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if ok && function.Name.Name == name && function.Body != nil {
			return function
		}
	}
	return nil
}

func cleanCutoverParameterNames(function *ast.FuncDecl) []string {
	if function == nil || function.Type == nil || function.Type.Params == nil {
		return nil
	}
	var names []string
	for _, field := range function.Type.Params.List {
		for _, name := range field.Names {
			names = append(names, name.Name)
		}
	}
	return names
}

func collectRejectedParentActionKinds(body *ast.BlockStmt, kinds map[string]bool, executionParam string) {
	ast.Inspect(body, func(node ast.Node) bool {
		switchStatement, ok := node.(*ast.SwitchStmt)
		if !ok || !switchesOnIdentifier(switchStatement, executionParam) {
			return true
		}
		collectRejectedSwitchKinds(switchStatement, kinds)
		return true
	})
}

func switchesOnIdentifier(statement *ast.SwitchStmt, name string) bool {
	identifier, ok := statement.Tag.(*ast.Ident)
	return ok && identifier.Name == name
}

func collectRejectedSwitchKinds(statement *ast.SwitchStmt, kinds map[string]bool) {
	for _, item := range statement.Body.List {
		clause, ok := item.(*ast.CaseClause)
		if !ok || !returnsBoolean(clause.Body, true) {
			continue
		}
		for _, expression := range clause.List {
			identifier, ok := expression.(*ast.Ident)
			if ok {
				kinds[identifier.Name] = true
			}
		}
	}
}

func collectRejectedParentActionNames(body *ast.BlockStmt, actions map[string]bool, actionParam string) {
	ast.Inspect(body, func(node ast.Node) bool {
		result, ok := node.(*ast.ReturnStmt)
		if !ok {
			return true
		}
		for _, expression := range result.Results {
			if action, ok := parentActionRejectedAction(expression, actionParam); ok {
				actions[action] = true
			}
		}
		return true
	})
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

func parentActionRejectedAction(expression ast.Expr, actionParam string) (string, bool) {
	comparison, ok := expression.(*ast.BinaryExpr)
	if !ok || comparison.Op != token.EQL {
		return "", false
	}
	if identifier, ok := comparison.X.(*ast.Ident); ok && identifier.Name == actionParam {
		return cleanCutoverStringLiteral(comparison.Y)
	}
	if identifier, ok := comparison.Y.(*ast.Ident); ok && identifier.Name == actionParam {
		return cleanCutoverStringLiteral(comparison.X)
	}
	return "", false
}

func cleanCutoverStringLiteral(expression ast.Expr) (string, bool) {
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
	for _, registry := range parentActionRegistries(file) {
		for _, element := range registry.Elts {
			entry, ok := element.(*ast.KeyValueExpr)
			if !ok || !parentActionRegistryEntryRejected(entry, rejectedKinds, rejectedActions) {
				continue
			}
			position := set.Position(entry.Key.Pos())
			violations = append(violations, Violation{
				Rule: cleanCutoverRule, Path: parentActionMetadataPath, Line: position.Line, Column: position.Column,
				Message: "registered parent action is retained solely behind an unconditional cutover rejection; remove the registry root",
			})
		}
	}
	return violations
}

func parentActionRegistries(file *ast.File) []*ast.CompositeLit {
	var registries []*ast.CompositeLit
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
			if ok {
				registries = append(registries, registry)
			}
		}
	}
	return registries
}

func parentActionRegistryEntryRejected(entry *ast.KeyValueExpr, rejectedKinds, rejectedActions map[string]bool) bool {
	if action, ok := cleanCutoverStringLiteral(entry.Key); ok && rejectedActions[action] {
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
