package harnesslint

import (
	"go/ast"
	"go/token"
	"strings"
)

type forwardOnlyAuthorityFallbackCandidate struct {
	branch *ast.IfStmt
	arm    ast.Node
}

const (
	forwardOnlyAuthorityFallbackWalkLimit = 4
	forwardOnlyNilIdentifier              = "nil"
)

func scanForwardOnlyAuthorityFallbackCompatibility(root string, paths []string) ([]Violation, error) {
	packages, err := parseForwardOnlySemanticPackages(root, paths)
	if err != nil {
		return nil, err
	}
	var violations []Violation
	for _, pkg := range packages {
		violations = append(violations, forwardOnlyAuthorityPackageViolations(pkg)...)
	}
	return violations, nil
}

func forwardOnlyAuthorityPackageViolations(pkg *forwardOnlySemanticPackage) []Violation {
	var violations []Violation
	for _, functions := range pkg.functions {
		for _, function := range functions {
			if function.file.test {
				continue
			}
			violations = append(violations, forwardOnlyAuthorityFallbackViolations(pkg, function)...)
		}
	}
	return violations
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
		if ok {
			candidates = append(candidates, forwardOnlyAuthorityCandidatesInBlock(block, authorityNames)...)
		}
		return true
	})
	return candidates
}

func forwardOnlyAuthorityCandidatesInBlock(block *ast.BlockStmt, authorityNames map[string]bool) []forwardOnlyAuthorityFallbackCandidate {
	var candidates []forwardOnlyAuthorityFallbackCandidate
	for index, statement := range block.List {
		branch, ok := statement.(*ast.IfStmt)
		if !ok {
			continue
		}
		candidate, ok := forwardOnlyAuthorityCandidateForBranch(branch, block.List[index+1:], authorityNames)
		if ok {
			candidates = append(candidates, candidate)
		}
	}
	return candidates
}

func forwardOnlyAuthorityCandidateForBranch(branch *ast.IfStmt, suffix []ast.Stmt, authorityNames map[string]bool) (forwardOnlyAuthorityFallbackCandidate, bool) {
	polarity := forwardOnlyAuthorityUnavailablePolarity(branch.Cond, authorityNames)
	if polarity == 1 {
		return forwardOnlyAuthorityFallbackCandidate{branch: branch, arm: branch.Body}, true
	}
	if polarity != -1 {
		return forwardOnlyAuthorityFallbackCandidate{}, false
	}
	if branch.Else != nil {
		return forwardOnlyAuthorityFallbackCandidate{branch: branch, arm: branch.Else}, true
	}
	if !forwardOnlyBlockReturns(branch.Body) || len(suffix) == 0 {
		return forwardOnlyAuthorityFallbackCandidate{}, false
	}
	return forwardOnlyAuthorityFallbackCandidate{branch: branch, arm: &ast.BlockStmt{List: suffix}}, true
}

func forwardOnlyBlockReturns(block *ast.BlockStmt) bool {
	if block == nil || len(block.List) == 0 {
		return false
	}
	_, ok := block.List[len(block.List)-1].(*ast.ReturnStmt)
	return ok
}

func forwardOnlyAuthorityUnavailablePolarity(expression ast.Expr, authorityNames map[string]bool) int {
	expression = forwardOnlyUnparen(expression)
	if unary, ok := expression.(*ast.UnaryExpr); ok {
		if unary.Op == token.NOT && forwardOnlyAuthorityReadinessExpression(unary.X, authorityNames) {
			return 1
		}
		return 0
	}
	if binary, ok := expression.(*ast.BinaryExpr); ok {
		return forwardOnlyAuthorityBinaryPolarity(binary, authorityNames)
	}
	if forwardOnlyAuthorityReadinessExpression(expression, authorityNames) {
		return -1
	}
	return 0
}

func forwardOnlyAuthorityBinaryPolarity(binary *ast.BinaryExpr, authorityNames map[string]bool) int {
	if binary.Op == token.LAND || binary.Op == token.LOR {
		left := forwardOnlyAuthorityUnavailablePolarity(binary.X, authorityNames)
		right := forwardOnlyAuthorityUnavailablePolarity(binary.Y, authorityNames)
		if left != 0 && left == right {
			return left
		}
		return 0
	}
	if binary.Op != token.EQL && binary.Op != token.NEQ {
		return 0
	}
	if polarity := forwardOnlyAuthorityComparisonPolarity(binary.Op, binary.X, binary.Y, authorityNames); polarity != 0 {
		return polarity
	}
	return forwardOnlyAuthorityComparisonPolarity(binary.Op, binary.Y, binary.X, authorityNames)
}

func forwardOnlyAuthorityComparisonPolarity(operator token.Token, subject, value ast.Expr, authorityNames map[string]bool) int {
	identifier, ok := forwardOnlyUnparen(value).(*ast.Ident)
	if !ok {
		return 0
	}
	if !forwardOnlyAuthorityComparisonSubject(subject, identifier.Name, authorityNames) {
		return 0
	}
	unavailable := identifier.Name == forwardOnlyNilIdentifier || identifier.Name == "false"
	if identifier.Name != forwardOnlyNilIdentifier && identifier.Name != "false" && identifier.Name != "true" {
		return 0
	}
	if operator == token.NEQ {
		unavailable = !unavailable
	}
	if unavailable {
		return 1
	}
	return -1
}

func forwardOnlyAuthorityComparisonSubject(subject ast.Expr, literal string, authorityNames map[string]bool) bool {
	if literal == forwardOnlyNilIdentifier {
		return forwardOnlyAuthorityIdentityExpression(subject, authorityNames)
	}
	return forwardOnlyAuthorityReadinessExpression(subject, authorityNames)
}

func forwardOnlyAuthorityIdentityExpression(expression ast.Expr, authorityNames map[string]bool) bool {
	identifier, ok := forwardOnlyUnparen(expression).(*ast.Ident)
	return ok && authorityNames[identifier.Name]
}

func forwardOnlyAuthorityReadinessExpression(expression ast.Expr, authorityNames map[string]bool) bool {
	switch typed := forwardOnlyUnparen(expression).(type) {
	case *ast.Ident:
		return authorityNames[typed.Name] && forwardOnlyReadinessName(typed.Name)
	case *ast.SelectorExpr:
		return forwardOnlyReadinessName(typed.Sel.Name) && forwardOnlyExpressionReferencesNames(typed.X, authorityNames)
	case *ast.CallExpr:
		return forwardOnlyAuthorityReadinessCall(typed, authorityNames)
	default:
		return false
	}
}

func forwardOnlyAuthorityReadinessCall(call *ast.CallExpr, authorityNames map[string]bool) bool {
	selector, ok := forwardOnlyUnparen(call.Fun).(*ast.SelectorExpr)
	if !ok || !forwardOnlyReadinessName(selector.Sel.Name) {
		return false
	}
	return forwardOnlyExpressionReferencesNames(selector.X, authorityNames)
}

func forwardOnlyReadinessName(name string) bool {
	lower := strings.ToLower(name)
	return containsAny(lower, "active", "available", "enabled", "ready", "applicable", "present", "attached")
}

func forwardOnlyAuthorityNames(function *forwardOnlySemanticFunction) map[string]bool {
	return forwardOnlyTrackedNames(function, forwardOnlyNodeHasControlSemantic, forwardOnlyAuthoritySource, forwardOnlyCollectAuthorityValueSpec)
}

func forwardOnlyTrackedNames(
	function *forwardOnlySemanticFunction,
	typePredicate func(ast.Node) bool,
	sourcePredicate func(ast.Expr, map[string]bool) bool,
	valueCollector func(*ast.ValueSpec, map[string]bool),
) map[string]bool {
	names := make(map[string]bool)
	forwardOnlyCollectTypedNames(function.decl.Recv, names, typePredicate)
	if function.decl.Type != nil {
		forwardOnlyCollectTypedNames(function.decl.Type.Params, names, typePredicate)
	}
	ast.Inspect(function.decl.Body, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.AssignStmt:
			forwardOnlyCollectAssignmentNames(typed, names, sourcePredicate)
		case *ast.ValueSpec:
			valueCollector(typed, names)
		}
		return true
	})
	return names
}

func forwardOnlyCollectTypedNames(fields *ast.FieldList, names map[string]bool, predicate func(ast.Node) bool) {
	if fields == nil {
		return
	}
	for _, field := range fields.List {
		if !predicate(field.Type) {
			continue
		}
		for _, name := range field.Names {
			if name.Name != "_" {
				names[name.Name] = true
			}
		}
	}
}

func forwardOnlyCollectAssignmentNames(assignment *ast.AssignStmt, names map[string]bool, predicate func(ast.Expr, map[string]bool) bool) {
	for index, target := range assignment.Lhs {
		identifier, ok := forwardOnlyUnparen(target).(*ast.Ident)
		if !ok || identifier.Name == "_" || names[identifier.Name] {
			continue
		}
		if forwardOnlyAssignmentSource(assignment.Rhs, index, len(assignment.Lhs), names, predicate) {
			names[identifier.Name] = true
		}
	}
}

func forwardOnlyCollectAuthorityValueSpec(spec *ast.ValueSpec, names map[string]bool) {
	for index, name := range spec.Names {
		if name.Name == "_" || names[name.Name] {
			continue
		}
		if forwardOnlyAssignmentSource(spec.Values, index, len(spec.Names), names, forwardOnlyAuthoritySource) {
			names[name.Name] = true
		}
	}
}

func forwardOnlyAssignmentSource(expressions []ast.Expr, index, targets int, names map[string]bool, predicate func(ast.Expr, map[string]bool) bool) bool {
	if len(expressions) == targets && index < len(expressions) {
		return predicate(expressions[index], names)
	}
	if len(expressions) == 1 && index == 0 {
		return predicate(expressions[0], names)
	}
	return false
}

func forwardOnlyAuthoritySource(expression ast.Expr, authorityNames map[string]bool) bool {
	return forwardOnlyExpressionReferencesNames(expression, authorityNames) || forwardOnlyNodeHasControlSemantic(expression)
}

func forwardOnlyNodeHasControlSemantic(node ast.Node) bool {
	if node == nil {
		return false
	}
	found := false
	ast.Inspect(node, func(current ast.Node) bool {
		identifier, ok := current.(*ast.Ident)
		if ok && forwardOnlyControlSemanticName(identifier.Name) {
			found = true
		}
		return !found
	})
	return found
}

func forwardOnlyControlSemanticName(name string) bool {
	lower := strings.ToLower(name)
	if strings.Contains(lower, "harness") && !strings.Contains(lower, "evidence") {
		return true
	}
	return containsAny(lower, "controller", "controlplane", "coordinator", "supervisor", "kernel")
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

func forwardOnlyFallbackReachesRetiredMutation(pkg *forwardOnlySemanticPackage, function *forwardOnlySemanticFunction, node ast.Node, remaining int, seen map[*forwardOnlySemanticFunction]bool) bool {
	if forwardOnlyDirectRetiredAuthorityMutation(function, node) {
		return true
	}
	if remaining == 0 {
		return false
	}
	return forwardOnlyFallbackCalleeMutation(pkg, function, node, remaining, seen)
}

func forwardOnlyFallbackCalleeMutation(pkg *forwardOnlySemanticPackage, function *forwardOnlySemanticFunction, node ast.Node, remaining int, seen map[*forwardOnlySemanticFunction]bool) bool {
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
	stateMutation, stateLock, stateAdmission := false, false, false
	ast.Inspect(node, func(current ast.Node) bool {
		call, ok := current.(*ast.CallExpr)
		if !ok {
			return true
		}
		mutation, lock, admission := forwardOnlyRetiredAuthorityCallKinds(call, stateVars)
		stateMutation = stateMutation || mutation
		stateLock = stateLock || lock
		stateAdmission = stateAdmission || admission
		return !stateMutation && (!stateLock || !stateAdmission)
	})
	return stateMutation || stateLock && stateAdmission
}

func forwardOnlyRetiredAuthorityCallKinds(call *ast.CallExpr, stateVars map[string]bool) (bool, bool, bool) {
	name := callableName(call.Fun)
	selector, selectorCall := forwardOnlyUnparen(call.Fun).(*ast.SelectorExpr)
	mutation := selectorCall && forwardOnlyExpressionReferencesNames(selector.X, stateVars) && forwardOnlyStateMutationName(selector.Sel.Name)
	if !forwardOnlyCallReferencesNames(call, stateVars) {
		return mutation, false, false
	}
	lower := strings.ToLower(name)
	lock := strings.Contains(lower, "acquirerepolock") || strings.Contains(lower, "repolock")
	admission := strings.Contains(lower, "admit") || strings.Contains(lower, "admission")
	return mutation, lock, admission
}

func forwardOnlyStateStoreVariables(function *forwardOnlySemanticFunction) map[string]bool {
	return forwardOnlyTrackedNames(function, forwardOnlyStateStoreType, forwardOnlyStateStoreSource, forwardOnlyCollectStateStoreValueSpec)
}

func forwardOnlyStateStoreType(node ast.Node) bool {
	return forwardOnlyTypeHasName(node, "statestore")
}

func forwardOnlyCollectStateStoreValueSpec(spec *ast.ValueSpec, names map[string]bool) {
	if forwardOnlyStateStoreType(spec.Type) {
		for _, name := range spec.Names {
			if name.Name != "_" {
				names[name.Name] = true
			}
		}
		return
	}
	for index, name := range spec.Names {
		if name.Name == "_" || names[name.Name] {
			continue
		}
		if forwardOnlyAssignmentSource(spec.Values, index, len(spec.Names), names, forwardOnlyStateStoreSource) {
			names[name.Name] = true
		}
	}
}

func forwardOnlyStateStoreSource(expression ast.Expr, stateVars map[string]bool) bool {
	if forwardOnlyExpressionReferencesNames(expression, stateVars) {
		return true
	}
	call, ok := forwardOnlyUnparen(expression).(*ast.CallExpr)
	return ok && strings.Contains(strings.ToLower(callableName(call.Fun)), "newstatestore")
}

func forwardOnlyTypeHasName(node ast.Node, fragment string) bool {
	if node == nil {
		return false
	}
	found := false
	ast.Inspect(node, func(current ast.Node) bool {
		identifier, ok := current.(*ast.Ident)
		if ok && strings.Contains(strings.ToLower(identifier.Name), fragment) {
			found = true
		}
		return !found
	})
	return found
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
	if strings.Contains(lower, "newstatestore") || forwardOnlyStateReadName(lower) {
		return false
	}
	return containsAny(lower,
		"save", "write", "persist", "commit", "record", "set", "update", "delete", "remove", "clear",
		"append", "mark", "rotate", "install", "adopt", "promot", "migrat", "acknowledge", "secure", "prepare",
	)
}

func forwardOnlyStateReadName(lower string) bool {
	return strings.HasPrefix(lower, "load") || strings.HasPrefix(lower, "read") || strings.HasPrefix(lower, "get") ||
		strings.HasPrefix(lower, "resolve") || strings.HasPrefix(lower, "capture") || strings.HasPrefix(lower, "inspect") ||
		strings.HasPrefix(lower, "check") || strings.HasPrefix(lower, "validate")
}
