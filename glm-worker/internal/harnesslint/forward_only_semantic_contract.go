package harnesslint

import (
	"go/ast"
	"go/token"
	"strconv"
)

func forwardOnlySemanticTestViolations(pkg *forwardOnlySemanticPackage) []Violation {
	var violations []Violation
	for _, functions := range pkg.functions {
		for _, function := range functions {
			if !function.file.test || !stringsHasTestPrefix(function.name) {
				continue
			}
			if forwardOnlySemanticTestAcceptsBothByCount(pkg, function) || forwardOnlySemanticTestAcceptsBothByBoolean(pkg, function) {
				violations = append(violations, forwardOnlyViolation(function.file.set, function.file.path, function.decl.Name,
					"tests must not guarantee successful acceptance of both old and current parent-wait transports"))
			}
		}
	}
	return violations
}

func stringsHasTestPrefix(name string) bool {
	return len(name) >= 4 && name[:4] == "Test"
}

func forwardOnlySemanticTestAcceptsBothByCount(pkg *forwardOnlySemanticPackage, function *forwardOnlySemanticFunction) bool {
	oldInputs, currentInputs := forwardOnlySemanticWaitInputCounts(pkg, function.decl.Body)
	if oldInputs == 0 || currentInputs == 0 {
		return false
	}
	expected, ok := forwardOnlySemanticAcceptedCount(function.decl.Body)
	return ok && expected == oldInputs+currentInputs && expected > currentInputs
}

func forwardOnlySemanticWaitInputCounts(pkg *forwardOnlySemanticPackage, node ast.Node) (int, int) {
	oldInputs := 0
	currentInputs := 0
	ast.Inspect(node, func(current ast.Node) bool {
		literal, ok := current.(*ast.CompositeLit)
		if !ok {
			return true
		}
		switch forwardOnlySemanticWaitInputKind(pkg, literal) {
		case forwardOnlyOldTransport:
			oldInputs++
		case forwardOnlyCurrentTransport:
			currentInputs++
		}
		return true
	})
	return oldInputs, currentInputs
}

func forwardOnlySemanticWaitInputKind(pkg *forwardOnlySemanticPackage, literal *ast.CompositeLit) uint8 {
	fields := forwardOnlySemanticCompositeFields(pkg, literal)
	typeValue := fields["Type"]
	if typeValue == "" {
		typeValue = fields["type"]
	}
	nameValue := fields["Name"]
	if nameValue == "" {
		nameValue = fields["name"]
	}
	inputValue := fields["Input"]
	if inputValue == "" {
		inputValue = fields["input"]
	}
	switch {
	case typeValue == "function_call" && nameValue == "wait":
		return forwardOnlyOldTransport
	case typeValue == "custom_tool_call" && nameValue == "exec" && containsWriteStdin(inputValue):
		return forwardOnlyCurrentTransport
	default:
		return 0
	}
}

func containsWriteStdin(value string) bool {
	for index := 0; index+17 <= len(value); index++ {
		if value[index:index+17] == "tools.write_stdin" {
			return true
		}
	}
	return false
}

func forwardOnlySemanticAcceptedCount(body *ast.BlockStmt) (int, bool) {
	expected := 0
	found := false
	ast.Inspect(body, func(node ast.Node) bool {
		branch, ok := node.(*ast.IfStmt)
		if !ok || !forwardOnlySemanticTestingFailure(branch.Body) {
			return true
		}
		if value, ok := forwardOnlySemanticCountExpectation(branch.Cond); ok {
			expected = value
			found = true
			return false
		}
		return true
	})
	return expected, found
}

func forwardOnlySemanticCountExpectation(expression ast.Expr) (int, bool) {
	binary, ok := forwardOnlyUnparen(expression).(*ast.BinaryExpr)
	if !ok {
		return 0, false
	}
	if binary.Op == token.LAND || binary.Op == token.LOR {
		if value, ok := forwardOnlySemanticCountExpectation(binary.X); ok {
			return value, true
		}
		return forwardOnlySemanticCountExpectation(binary.Y)
	}
	if binary.Op != token.NEQ {
		return 0, false
	}
	if forwardOnlySemanticCountExpression(binary.X) {
		return forwardOnlySemanticInteger(binary.Y)
	}
	if forwardOnlySemanticCountExpression(binary.Y) {
		return forwardOnlySemanticInteger(binary.X)
	}
	return 0, false
}

func forwardOnlySemanticCountExpression(expression ast.Expr) bool {
	switch typed := forwardOnlyUnparen(expression).(type) {
	case *ast.SelectorExpr:
		return typed.Sel.Name == "Count"
	case *ast.CallExpr:
		identifier, ok := forwardOnlyUnparen(typed.Fun).(*ast.Ident)
		return ok && identifier.Name == "len" && len(typed.Args) == 1
	default:
		return false
	}
}

func forwardOnlySemanticInteger(expression ast.Expr) (int, bool) {
	literal, ok := forwardOnlyUnparen(expression).(*ast.BasicLit)
	if !ok || literal.Kind != token.INT {
		return 0, false
	}
	value, err := strconv.Atoi(literal.Value)
	return value, err == nil
}

func forwardOnlySemanticTestAcceptsBothByBoolean(pkg *forwardOnlySemanticPackage, function *forwardOnlySemanticFunction) bool {
	accepted := make(map[string]uint8)
	ast.Inspect(function.decl.Body, func(node ast.Node) bool {
		branch, ok := node.(*ast.IfStmt)
		if !ok || !forwardOnlySemanticTestingFailure(branch.Body) {
			return true
		}
		call := forwardOnlySemanticPositiveBooleanCall(branch.Cond)
		if call == nil {
			return true
		}
		name := forwardOnlySemanticCallName(call)
		if name == "" {
			return true
		}
		for _, argument := range call.Args {
			literal, ok := forwardOnlyUnparen(argument).(*ast.CompositeLit)
			if !ok {
				continue
			}
			accepted[name] |= forwardOnlySemanticWaitInputKind(pkg, literal)
		}
		return true
	})
	for _, kinds := range accepted {
		if kinds&(forwardOnlyOldTransport|forwardOnlyCurrentTransport) == forwardOnlyOldTransport|forwardOnlyCurrentTransport {
			return true
		}
	}
	return false
}

func forwardOnlySemanticPositiveBooleanCall(expression ast.Expr) *ast.CallExpr {
	unary, ok := forwardOnlyUnparen(expression).(*ast.UnaryExpr)
	if !ok || unary.Op != token.NOT {
		return nil
	}
	call, _ := forwardOnlyUnparen(unary.X).(*ast.CallExpr)
	return call
}

func forwardOnlySemanticCallName(call *ast.CallExpr) string {
	switch typed := forwardOnlyUnparen(call.Fun).(type) {
	case *ast.Ident:
		return typed.Name
	case *ast.SelectorExpr:
		return typed.Sel.Name
	default:
		return ""
	}
}

func forwardOnlySemanticTestingFailure(body *ast.BlockStmt) bool {
	failed := false
	ast.Inspect(body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := forwardOnlyUnparen(call.Fun).(*ast.SelectorExpr)
		if !ok {
			return true
		}
		switch selector.Sel.Name {
		case "Fatal", "Fatalf", "Error", "Errorf", "Fail", "FailNow":
			failed = true
			return false
		default:
			return true
		}
	})
	return failed
}
