package harnesslint

import (
	"go/ast"
	"go/token"
	"strconv"
)

type forwardOnlySemanticCountCandidate struct {
	old     int
	current int
}

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
	candidates := forwardOnlySemanticCountCandidates(pkg, function.decl.Body)
	if len(candidates) == 0 {
		return false
	}
	accepted := false
	ast.Inspect(function.decl.Body, func(node ast.Node) bool {
		branch, ok := node.(*ast.IfStmt)
		if !ok || !forwardOnlySemanticTestingFailure(branch.Body) {
			return true
		}
		subject, expected, ok := forwardOnlySemanticCountExpectation(branch.Cond)
		if !ok {
			return true
		}
		candidate, ok := candidates[subject]
		if ok && candidate.old > 0 && candidate.current > 0 && expected == candidate.old+candidate.current {
			accepted = true
			return false
		}
		return true
	})
	return accepted
}

func forwardOnlySemanticCountCandidates(pkg *forwardOnlySemanticPackage, body *ast.BlockStmt) map[string]forwardOnlySemanticCountCandidate {
	candidates := make(map[string]forwardOnlySemanticCountCandidate)
	ast.Inspect(body, func(node ast.Node) bool {
		assignment, ok := node.(*ast.AssignStmt)
		if !ok || len(assignment.Lhs) != 1 || len(assignment.Rhs) != 1 {
			return true
		}
		name, ok := forwardOnlyUnparen(assignment.Lhs[0]).(*ast.Ident)
		if !ok {
			return true
		}
		call, ok := forwardOnlyUnparen(assignment.Rhs[0]).(*ast.CallExpr)
		if !ok {
			return true
		}
		oldInputs, currentInputs := forwardOnlySemanticCallWaitInputCounts(pkg, call)
		if oldInputs > 0 && currentInputs > 0 {
			candidates[name.Name] = forwardOnlySemanticCountCandidate{old: oldInputs, current: currentInputs}
		}
		return true
	})
	return candidates
}

func forwardOnlySemanticCallWaitInputCounts(pkg *forwardOnlySemanticPackage, call *ast.CallExpr) (int, int) {
	oldInputs := 0
	currentInputs := 0
	for _, argument := range call.Args {
		ast.Inspect(argument, func(node ast.Node) bool {
			literal, ok := node.(*ast.CompositeLit)
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
	}
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

func forwardOnlySemanticCountExpectation(expression ast.Expr) (string, int, bool) {
	binary, ok := forwardOnlyUnparen(expression).(*ast.BinaryExpr)
	if !ok {
		return "", 0, false
	}
	if binary.Op == token.LAND || binary.Op == token.LOR {
		if subject, value, ok := forwardOnlySemanticCountExpectation(binary.X); ok {
			return subject, value, true
		}
		return forwardOnlySemanticCountExpectation(binary.Y)
	}
	if binary.Op != token.NEQ {
		return "", 0, false
	}
	if subject := forwardOnlySemanticCountSubject(binary.X); subject != "" {
		value, ok := forwardOnlySemanticInteger(binary.Y)
		return subject, value, ok
	}
	if subject := forwardOnlySemanticCountSubject(binary.Y); subject != "" {
		value, ok := forwardOnlySemanticInteger(binary.X)
		return subject, value, ok
	}
	return "", 0, false
}

func forwardOnlySemanticCountSubject(expression ast.Expr) string {
	switch typed := forwardOnlyUnparen(expression).(type) {
	case *ast.SelectorExpr:
		if typed.Sel.Name != "Count" {
			return ""
		}
		return forwardOnlySemanticObjectBase(typed.X)
	case *ast.CallExpr:
		identifier, ok := forwardOnlyUnparen(typed.Fun).(*ast.Ident)
		if !ok || identifier.Name != "len" || len(typed.Args) != 1 {
			return ""
		}
		argument := forwardOnlyUnparen(typed.Args[0])
		for {
			selector, ok := argument.(*ast.SelectorExpr)
			if !ok {
				break
			}
			argument = forwardOnlyUnparen(selector.X)
		}
		return forwardOnlySemanticObjectBase(argument)
	default:
		return ""
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
