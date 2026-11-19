package harnesslint

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
)

const (
	cleanCutoverRule         = "clean-cutover-rejection-only-surface"
	parentActionMetadataPath = "glm-worker/internal/parentactioncmd/command_metadata.go"
	parentActionRegistryName = "parentActionCommands"
)

func scanCleanCutover(root string, paths []string) ([]Violation, error) {
	for _, path := range paths {
		if path == parentActionMetadataPath {
			return cleanCutoverParentActionViolations(root)
		}
	}
	return nil, nil
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
	var violations []Violation
	report := func(node ast.Node, message string) {
		position := set.Position(node.Pos())
		violations = append(violations, Violation{
			Rule: cleanCutoverRule, Path: parentActionMetadataPath,
			Line: position.Line, Column: position.Column, Message: message,
		})
	}
	kinds, validDispatch := cleanCutoverExecutableKinds(file)
	if !validDispatch {
		report(file, "current parent-action command dispatch contract is not provable")
		return violations, nil
	}
	registry := cleanCutoverRegistry(file)
	if registry == nil {
		report(file, "canonical parent-action command registry is absent or not a literal map")
		return violations, nil
	}
	for _, element := range registry.Elts {
		cleanCutoverReportRegistryEntry(element, kinds, report)
	}
	return violations, nil
}

func cleanCutoverReportRegistryEntry(element ast.Expr, kinds map[string]bool, report func(ast.Node, string)) {
	entry, ok := element.(*ast.KeyValueExpr)
	if !ok {
		report(element, "parent-action registry entry must be a keyed descriptor")
		return
	}
	descriptor, ok := entry.Value.(*ast.CompositeLit)
	if !ok {
		report(entry, "parent-action registry descriptor must be a literal")
		return
	}
	key, keyOK := cleanCutoverAction(entry.Key)
	action, actionOK := cleanCutoverDescriptorField(descriptor, "Action")
	actionName, actionNameOK := cleanCutoverAction(action)
	if !keyOK || !actionOK || !actionNameOK || key != actionName {
		report(entry, "parent-action registry key and descriptor action must be the same canonical action")
	}
	execution, ok := cleanCutoverDescriptorField(descriptor, "Execute")
	if !ok || !cleanCutoverExecutable(execution, kinds) {
		report(entry, "registered parent action has no live executable dispatch (rejection-only tombstone)")
	}
	if terminal, specified := cleanCutoverDescriptorField(descriptor, "TerminalExecute"); specified &&
		!cleanCutoverExecutable(terminal, kinds) {
		report(entry, "registered parent action has no live terminal dispatch (rejection-only tombstone)")
	}
}

func cleanCutoverAction(expr ast.Expr) (string, bool) {
	switch value := expr.(type) {
	case *ast.Ident:
		return value.Name, true
	case *ast.BasicLit:
		if value.Kind == token.STRING {
			s, err := strconv.Unquote(value.Value)
			return s, err == nil
		}
	}
	return "", false
}

func cleanCutoverDescriptorField(descriptor *ast.CompositeLit, name string) (ast.Expr, bool) {
	for _, element := range descriptor.Elts {
		field, ok := element.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		identifier, ok := field.Key.(*ast.Ident)
		if ok && identifier.Name == name {
			return field.Value, true
		}
	}
	return nil, false
}

func cleanCutoverExecutable(expr ast.Expr, kinds map[string]bool) bool {
	identifier, ok := expr.(*ast.Ident)
	return ok && identifier.Name != "parentActionExecutionUnsupported" && kinds[identifier.Name]
}

func cleanCutoverRegistry(file *ast.File) *ast.CompositeLit {
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
				return registry
			}
		}
	}
	return nil
}

func cleanCutoverExecutableKinds(file *ast.File) (map[string]bool, bool) {
	parent := cleanCutoverFunction(file, "executeParentActionCommand")
	standard := cleanCutoverFunction(file, "executeStandardParentAction")
	interfaced := cleanCutoverFunction(file, "executeInterfaceParentAction")
	special := cleanCutoverFunction(file, "executeSpecialParentAction")
	if !cleanCutoverCalls(parent, "executeStandardParentAction") ||
		!cleanCutoverCalls(parent, "executeSpecialParentAction") ||
		!cleanCutoverCalls(standard, "executeInterfaceParentAction") ||
		interfaced == nil || special == nil {
		return nil, false
	}
	kinds := make(map[string]bool)
	cleanCutoverStandardKinds(standard, kinds)
	cleanCutoverInterfaceKinds(interfaced, kinds)
	cleanCutoverSpecialKinds(special, kinds)
	return kinds, len(kinds) > 0
}

func cleanCutoverStandardKinds(function *ast.FuncDecl, kinds map[string]bool) {
	for _, statement := range function.Body.List {
		conditional, ok := statement.(*ast.IfStmt)
		if !ok {
			continue
		}
		if kind, ok := cleanCutoverExecutionMatch(conditional.Cond); ok && cleanCutoverHandlerReturn(conditional.Body.List, true) {
			kinds[kind] = true
		}
	}
}

func cleanCutoverInterfaceKinds(function *ast.FuncDecl, kinds map[string]bool) {
	for _, statement := range function.Body.List {
		switcher, ok := statement.(*ast.SwitchStmt)
		if !ok {
			continue
		}
		tag, ok := switcher.Tag.(*ast.Ident)
		if !ok || tag.Name != cleanCutoverExecutionIdentifier {
			continue
		}
		for _, item := range switcher.Body.List {
			cleanCutoverCaseKinds(item, kinds)
		}
	}
}

func cleanCutoverCaseKinds(item ast.Stmt, kinds map[string]bool) {
	clause, ok := item.(*ast.CaseClause)
	if !ok || !cleanCutoverHandlerReturn(clause.Body, true) {
		return
	}
	for _, expression := range clause.List {
		if kind, ok := expression.(*ast.Ident); ok {
			kinds[kind.Name] = true
		}
	}
}

func cleanCutoverSpecialKinds(function *ast.FuncDecl, kinds map[string]bool) {
	for _, statement := range function.Body.List {
		conditional, ok := statement.(*ast.IfStmt)
		if !ok {
			continue
		}
		if kind, ok := cleanCutoverExecutionMatch(conditional.Cond); ok && cleanCutoverHandlerReturn(conditional.Body.List, false) {
			kinds[kind] = true
		}
	}
}

const cleanCutoverExecutionIdentifier = "execution"

func cleanCutoverExecutionMatch(expression ast.Expr) (string, bool) {
	comparison, ok := expression.(*ast.BinaryExpr)
	if !ok || comparison.Op != token.EQL {
		return "", false
	}
	if execution, ok := comparison.X.(*ast.Ident); ok && execution.Name == cleanCutoverExecutionIdentifier {
		if kind, ok := comparison.Y.(*ast.Ident); ok {
			return kind.Name, true
		}
	}
	if execution, ok := comparison.Y.(*ast.Ident); ok && execution.Name == cleanCutoverExecutionIdentifier {
		if kind, ok := comparison.X.(*ast.Ident); ok {
			return kind.Name, true
		}
	}
	return "", false
}

func cleanCutoverHandlerReturn(statements []ast.Stmt, handled bool) bool {
	if len(statements) != 1 {
		return false
	}
	result, ok := statements[0].(*ast.ReturnStmt)
	if !ok || len(result.Results) != 1 && (!handled || len(result.Results) != 2) {
		return false
	}
	call, ok := result.Results[0].(*ast.CallExpr)
	if !ok {
		return false
	}
	handler, ok := call.Fun.(*ast.Ident)
	if !ok || !strings.HasPrefix(handler.Name, "execute") {
		return false
	}
	if !handled {
		return len(result.Results) == 1
	}
	if len(result.Results) != 2 {
		return false
	}
	accepted, ok := result.Results[1].(*ast.Ident)
	return ok && accepted.Name == "true"
}

func cleanCutoverFunction(file *ast.File, name string) *ast.FuncDecl {
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if ok && function.Name.Name == name && function.Body != nil {
			return function
		}
	}
	return nil
}

func cleanCutoverCalls(function *ast.FuncDecl, name string) bool {
	if function == nil {
		return false
	}
	found := false
	ast.Inspect(function.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		identifier, ok := call.Fun.(*ast.Ident)
		if ok && identifier.Name == name {
			found = true
		}
		return !found
	})
	return found
}
