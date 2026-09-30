package harnesslint

import (
	"go/ast"
	"go/token"
	"strings"
)

const forwardOnlyAuthorityFallbackWalkLimit = 4

type forwardOnlyAuthorityFallbackCandidate struct {
	branch *ast.IfStmt
	arm    ast.Node
}

func scanForwardOnlyAuthorityFallbackCompatibility(root string, paths []string) ([]Violation, error) {
	packages, err := parseForwardOnlySemanticPackages(root, paths)
	if err != nil {
		return nil, err
	}
	var violations []Violation
	for _, pkg := range packages {
		for _, functions := range pkg.functions {
			for _, function := range functions {
				if function.file.test {
					continue
				}
				violations = append(violations, forwardOnlyAuthorityFallbackViolations(pkg, function)...)
			}
		}
	}
	return violations, nil
}

func forwardOnlyAuthorityFallbackViolations(pkg *forwardOnlySemanticPackage, function *forwardOnlySemanticFunction) []Violation {
	authorityNames := forwardOnlyAuthorityNames(function)
	if len(authorityNames) == 0 {
		return nil
	}
	var violations []Violation
	for _, candidate := range forwardOnlyAuthorityFallbackCandidates(function.decl.Body, authorityNames) {
		if !forwardOnlyFallbackReachesRetiredMutation(pkg, function, candidate.arm, forwardOnlyAuthorityFallbackWalkLimit, nil) {
			continue
		}
		violations = append(violations, forwardOnlyViolation(
			function.file.set,
			function.file.path,
			candidate.branch,
			"new mutation authority must fail closed instead of falling back to StateStore/path-local mutation authority",
		))
	}
	return violations
}

func forwardOnlyAuthorityFallbackCandidates(body *ast.BlockStmt, authorityNames map[string]bool) []forwardOnlyAuthorityFallbackCandidate {
	var candidates []forwardOnlyAuthorityFallbackCandidate
	ast.Inspect(body, func(node ast.Node) bool {
		block, ok := node.(*ast.BlockStmt)
		if !ok {
			return true
		}
		for index, statement := range block.List {
			branch, ok := statement.(*ast.IfStmt)
			if !ok {
				continue
			}
			polarity := forwardOnlyAuthorityUnavailablePolarity(branch.Cond, authorityNames)
			switch polarity {
			case 1:
				candidates = append(candidates, forwardOnlyAuthorityFallbackCandidate{branch: branch, arm: branch.Body})
			case -1:
				if branch.Else != nil {
					candidates = append(candidates, forwardOnlyAuthorityFallbackCandidate{branch: branch, arm: branch.Else})
					continue
				}
				if forwardOnlyBlockTerminates(branch.Body) && index+1 < len(block.List) {
					candidates = append(candidates, forwardOnlyAuthorityFallbackCandidate{
						branch: branch,
						arm:    &ast.BlockStmt{List: block.List[index+1:]},
					})
				}
			}
		}
		return true
	})
	return candidates
}

func forwardOnlyBlockTerminates(block *ast.BlockStmt) bool {
	if block == nil || len(block.List) == 0 {
		return false
	}
	switch block.List[len(block.List)-1].(type) {
	case *ast.ReturnStmt, *ast.BranchStmt:
		return true
	default:
		return false
	}
}

// The polarity is 1 when the condition is true for unavailable/inactive
// authority and -1 when it is true for available/active authority.
func forwardOnlyAuthorityUnavailablePolarity(expression ast.Expr, authorityNames map[string]bool) int {
	expression = forwardOnlyUnparen(expression)
	switch typed := expression.(type) {
	case *ast.UnaryExpr:
		if typed.Op == token.NOT && forwardOnlyAuthorityReadinessExpression(typed.X, authorityNames) {
			return 1
		}
	case *ast.BinaryExpr:
		if typed.Op == token.LAND || typed.Op == token.LOR {
			left := forwardOnlyAuthorityUnavailablePolarity(typed.X, authorityNames)
			right := forwardOnlyAuthorityUnavailablePolarity(typed.Y, authorityNames)
			if left != 0 && left == right {
				return left
			}
			return 0
		}
		if typed.Op != token.EQL && typed.Op != token.NEQ {
			return 0
		}
		if polarity := forwardOnlyAuthorityComparisonPolarity(typed.Op, typed.X, typed.Y, authorityNames); polarity != 0 {
			return polarity
		}
		return forwardOnlyAuthorityComparisonPolarity(typed.Op, typed.Y, typed.X, authorityNames)
	default:
		if forwardOnlyAuthorityReadinessExpression(expression, authorityNames) {
			return -1
		}
	}
	return 0
}

func forwardOnlyAuthorityComparisonPolarity(operator token.Token, authority, value ast.Expr, authorityNames map[string]bool) int {
	if !forwardOnlyAuthorityReadinessExpression(authority, authorityNames) {
		return 0
	}
	identifier, ok := forwardOnlyUnparen(value).(*ast.Ident)
	if !ok {
		return 0
	}
	var trueMeansUnavailable bool
	switch identifier.Name {
	case "nil", "false":
		trueMeansUnavailable = true
	case "true":
		trueMeansUnavailable = false
	default:
		return 0
	}
	if operator == token.NEQ {
		trueMeansUnavailable = !trueMeansUnavailable
	}
	if trueMeansUnavailable {
		return 1
	}
	return -1
}

func forwardOnlyAuthorityReadinessExpression(expression ast.Expr, authorityNames map[string]bool) bool {
	found := false
	ast.Inspect(expression, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.Ident:
			if authorityNames[typed.Name] || forwardOnlyAuthoritySemanticName(typed.Name) {
				found = true
			}
		case *ast.SelectorExpr:
			if forwardOnlyAuthoritySemanticName(typed.Sel.Name) || forwardOnlyExpressionReferencesNames(typed.X, authorityNames) {
				found = true
			}
		}
		return !found
	})
	return found
}

func forwardOnlyAuthorityNames(function *forwardOnlySemanticFunction) map[string]bool {
	names := make(map[string]bool)
	forwardOnlyCollectAuthorityTypedNames(function.decl.Recv, names)
	if function.decl.Type != nil && function.decl.Type.Params != nil {
		forwardOnlyCollectAuthorityTypedNames(function.decl.Type.Params.List, names)
	}
	for changed := true; changed; {
		changed = false
		ast.Inspect(function.decl.Body, func(node ast.Node) bool {
			switch typed := node.(type) {
			case *ast.AssignStmt:
				if forwardOnlyAnyAuthoritySource(typed.Rhs, names) {
					for _, target := range typed.Lhs {
						identifier, ok := forwardOnlyUnparen(target).(*ast.Ident)
						if ok && identifier.Name != "_" && !names[identifier.Name] {
							names[identifier.Name] = true
							changed = true
						}
					}
				}
			case *ast.ValueSpec:
				if forwardOnlyTypeHasName(typed.Type, "statestore") {
					return true
				}
				if forwardOnlyAnyAuthoritySource(typed.Values, names) {
					for _, name := range typed.Names {
						if name.Name != "_" && !names[name.Name] {
							names[name.Name] = true
							changed = true
						}
					}
				}
			}
			return true
		})
	}
	return names
}

func forwardOnlyCollectAuthorityTypedNames(fields *ast.FieldList, names map[string]bool) {
	if fields == nil {
		return
	}
	for _, field := range fields.List {
		if !forwardOnlyNodeHasAuthoritySemantic(field.Type) {
			continue
		}
		for _, name := range field.Names {
			if name.Name != "_" {
				names[name.Name] = true
			}
		}
	}
}

func forwardOnlyAnyAuthoritySource(expressions []ast.Expr, names map[string]bool) bool {
	for _, expression := range expressions {
		if forwardOnlyNodeHasAuthoritySemantic(expression) || forwardOnlyExpressionReferencesNames(expression, names) {
			return true
		}
	}
	return false
}

func forwardOnlyNodeHasAuthoritySemantic(node ast.Node) bool {
	if node == nil {
		return false
	}
	found := false
	ast.Inspect(node, func(current ast.Node) bool {
		switch typed := current.(type) {
		case *ast.Ident:
			found = forwardOnlyAuthoritySemanticName(typed.Name)
		case *ast.SelectorExpr:
			found = forwardOnlyAuthoritySemanticName(typed.Sel.Name)
		}
		return !found
	})
	return found
}

func forwardOnlyAuthoritySemanticName(name string) bool {
	lower := strings.ToLower(name)
	return containsAny(lower, "controller", "authority", "harness", "controlplane", "coordinator", "supervisor", "kernel")
}

func forwardOnlyExpressionReferencesNames(node ast.Node, names map[string]bool) bool {
	found := false
	ast.Inspect(node, func(current ast.Node) bool {
		identifier, ok := current.(*ast.Ident)
		if ok && names[identifier.Name] {
			found = true
		}
		return !found
	})
	return found
}

func forwardOnlyFallbackReachesRetiredMutation(
	pkg *forwardOnlySemanticPackage,
	function *forwardOnlySemanticFunction,
	node ast.Node,
	remaining int,
	seen map[*forwardOnlySemanticFunction]bool,
) bool {
	if forwardOnlyDirectRetiredAuthorityMutation(function, node) {
		return true
	}
	if remaining == 0 {
		return false
	}
	if seen == nil {
		seen = make(map[*forwardOnlySemanticFunction]bool)
	}
	for _, call := range forwardOnlySemanticCalls(node) {
		callee := forwardOnlySemanticResolveCall(pkg, function.file, call)
		if callee == nil || callee.file.test || seen[callee] {
			continue
		}
		seen[callee] = true
		found := forwardOnlyFallbackReachesRetiredMutation(pkg, callee, callee.decl.Body, remaining-1, seen)
		delete(seen, callee)
		if found {
			return true
		}
	}
	return false
}

func forwardOnlyDirectRetiredAuthorityMutation(function *forwardOnlySemanticFunction, node ast.Node) bool {
	stateVars := forwardOnlyStateStoreVariables(function)
	if len(stateVars) == 0 {
		return false
	}
	stateMutation := false
	stateLock := false
	stateAdmission := false
	ast.Inspect(node, func(current ast.Node) bool {
		call, ok := current.(*ast.CallExpr)
		if !ok {
			return true
		}
		name := callableName(call.Fun)
		selector, selectorCall := forwardOnlyUnparen(call.Fun).(*ast.SelectorExpr)
		if selectorCall && forwardOnlyExpressionReferencesNames(selector.X, stateVars) && forwardOnlyStateMutationName(selector.Sel.Name) {
			stateMutation = true
		}
		if forwardOnlyCallReferencesNames(call, stateVars) {
			lower := strings.ToLower(name)
			if strings.Contains(lower, "acquirerepolock") || strings.Contains(lower, "repolock") {
				stateLock = true
			}
			if strings.Contains(lower, "admit") || strings.Contains(lower, "admission") {
				stateAdmission = true
			}
			if forwardOnlyStateMutationName(name) {
				stateMutation = true
			}
		}
		return !(stateMutation || (stateLock && stateAdmission))
	})
	return stateMutation || (stateLock && stateAdmission)
}

func forwardOnlyStateStoreVariables(function *forwardOnlySemanticFunction) map[string]bool {
	names := make(map[string]bool)
	forwardOnlyCollectStateStoreTypedNames(function.decl.Recv, names)
	if function.decl.Type != nil && function.decl.Type.Params != nil {
		forwardOnlyCollectStateStoreTypedNames(function.decl.Type.Params.List, names)
	}
	for changed := true; changed; {
		changed = false
		ast.Inspect(function.decl.Body, func(node ast.Node) bool {
			switch typed := node.(type) {
			case *ast.AssignStmt:
				for index, target := range typed.Lhs {
					identifier, ok := forwardOnlyUnparen(target).(*ast.Ident)
					if !ok || identifier.Name == "_" || names[identifier.Name] {
						continue
					}
					if forwardOnlyAssignmentStateStoreSource(typed.Rhs, index, len(typed.Lhs), names) {
						names[identifier.Name] = true
						changed = true
					}
				}
			case *ast.ValueSpec:
				if forwardOnlyTypeHasName(typed.Type, "statestore") {
					for _, name := range typed.Names {
						if name.Name != "_" && !names[name.Name] {
							names[name.Name] = true
							changed = true
						}
					}
				}
			}
			return true
		})
	}
	return names
}

func forwardOnlyCollectStateStoreTypedNames(fields *ast.FieldList, names map[string]bool) {
	if fields == nil {
		return
	}
	for _, field := range fields.List {
		if !forwardOnlyTypeHasName(field.Type, "statestore") {
			continue
		}
		for _, name := range field.Names {
			if name.Name != "_" {
				names[name.Name] = true
			}
		}
	}
}

func forwardOnlyTypeHasName(node ast.Node, fragment string) bool {
	if node == nil {
		return false
	}
	found := false
	ast.Inspect(node, func(current ast.Node) bool {
		switch typed := current.(type) {
		case *ast.Ident:
			found = strings.Contains(strings.ToLower(typed.Name), fragment)
		case *ast.SelectorExpr:
			found = strings.Contains(strings.ToLower(typed.Sel.Name), fragment)
		}
		return !found
	})
	return found
}

func forwardOnlyAssignmentStateStoreSource(expressions []ast.Expr, index, targets int, stateVars map[string]bool) bool {
	if len(expressions) == 0 {
		return false
	}
	expression := expressions[0]
	if len(expressions) == targets && index < len(expressions) {
		expression = expressions[index]
	}
	return forwardOnlyStateStoreSource(expression, stateVars)
}

func forwardOnlyStateStoreSource(expression ast.Expr, stateVars map[string]bool) bool {
	if forwardOnlyExpressionReferencesNames(expression, stateVars) {
		return true
	}
	call, ok := forwardOnlyUnparen(expression).(*ast.CallExpr)
	if !ok {
		return false
	}
	return strings.Contains(strings.ToLower(callableName(call.Fun)), "newstatestore")
}

func forwardOnlyCallReferencesNames(call *ast.CallExpr, names map[string]bool) bool {
	for _, argument := range call.Args {
		if forwardOnlyExpressionReferencesNames(argument, names) {
			return true
		}
	}
	return false
}

func forwardOnlyStateMutationName(name string) bool {
	lower := strings.ToLower(name)
	if strings.Contains(lower, "newstatestore") || strings.HasPrefix(lower, "load") || strings.HasPrefix(lower, "read") || strings.HasPrefix(lower, "get") || strings.HasPrefix(lower, "resolve") || strings.HasPrefix(lower, "capture") || strings.HasPrefix(lower, "inspect") || strings.HasPrefix(lower, "check") || strings.HasPrefix(lower, "validate") {
		return false
	}
	return containsAny(lower,
		"save", "write", "persist", "commit", "record", "set", "update", "delete", "remove", "clear",
		"append", "mark", "rotate", "install", "adopt", "promot", "migrat", "acknowledge", "secure", "prepare",
	)
}
