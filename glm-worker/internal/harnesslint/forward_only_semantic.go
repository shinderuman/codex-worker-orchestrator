package harnesslint

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"strconv"
	"strings"
)

type forwardOnlySemanticFile struct {
	path    string
	set     *token.FileSet
	file    *ast.File
	test    bool
	imports map[string]bool
}

type forwardOnlySemanticFunction struct {
	file   *forwardOnlySemanticFile
	decl   *ast.FuncDecl
	name   string
	params []string
}

type forwardOnlySemanticPackage struct {
	constants map[string]string
	functions map[string][]*forwardOnlySemanticFunction
	files     []*forwardOnlySemanticFile
}

type forwardOnlyTransportArm struct {
	kind   uint8
	object string
	node   ast.Node
}

const (
	forwardOnlyOldTransport uint8 = 1 << iota
	forwardOnlyCurrentTransport
	forwardOnlyOldWait
	forwardOnlyCurrentWait
	forwardOnlyTransportWalkLimit = 4
)

func scanForwardOnlySemanticCompatibility(root string, paths []string) ([]Violation, error) {
	packages, err := parseForwardOnlySemanticPackages(root, paths)
	if err != nil {
		return nil, err
	}
	var violations []Violation
	for _, pkg := range packages {
		violations = append(violations, forwardOnlySemanticProductionViolations(pkg)...)
		violations = append(violations, forwardOnlySemanticTestViolations(pkg)...)
	}
	return violations, nil
}

func parseForwardOnlySemanticPackages(root string, paths []string) (map[string]*forwardOnlySemanticPackage, error) {
	packages := make(map[string]*forwardOnlySemanticPackage)
	for _, filePath := range paths {
		if !strings.HasSuffix(filePath, ".go") || forwardOnlyFixturePath(filePath) {
			continue
		}
		parsed, key, err := parseForwardOnlySemanticFile(root, filePath)
		if err != nil {
			return nil, err
		}
		pkg := packages[key]
		if pkg == nil {
			pkg = &forwardOnlySemanticPackage{constants: make(map[string]string), functions: make(map[string][]*forwardOnlySemanticFunction)}
			packages[key] = pkg
		}
		pkg.files = append(pkg.files, parsed)
	}
	for _, pkg := range packages {
		indexForwardOnlySemanticPackage(pkg)
	}
	return packages, nil
}

func parseForwardOnlySemanticFile(root, filePath string) (*forwardOnlySemanticFile, string, error) {
	data, err := readRegularFile(root, filePath)
	if err != nil {
		return nil, "", err
	}
	set := token.NewFileSet()
	file, err := parser.ParseFile(set, filePath, data, 0)
	if err != nil {
		return nil, "", fmt.Errorf("parse %s: %w", filePath, err)
	}
	parsed := &forwardOnlySemanticFile{
		path:    filePath,
		set:     set,
		file:    file,
		test:    strings.HasSuffix(filePath, "_test.go"),
		imports: forwardOnlySemanticImportAliases(file),
	}
	return parsed, path.Dir(filePath) + "\x00" + file.Name.Name, nil
}

func forwardOnlySemanticImportAliases(file *ast.File) map[string]bool {
	aliases := make(map[string]bool)
	for _, spec := range file.Imports {
		if spec.Name != nil && spec.Name.Name != "_" && spec.Name.Name != "." {
			aliases[spec.Name.Name] = true
			continue
		}
		value, err := strconv.Unquote(spec.Path.Value)
		if err == nil {
			parts := strings.Split(value, "/")
			aliases[parts[len(parts)-1]] = true
		}
	}
	return aliases
}

func indexForwardOnlySemanticPackage(pkg *forwardOnlySemanticPackage) {
	for _, file := range pkg.files {
		forwardOnlySemanticCollectConstants(pkg, file.file)
		for _, declaration := range file.file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			indexed := &forwardOnlySemanticFunction{
				file:   file,
				decl:   function,
				name:   function.Name.Name,
				params: forwardOnlySemanticParameterNames(function),
			}
			pkg.functions[indexed.name] = append(pkg.functions[indexed.name], indexed)
		}
	}
}

func forwardOnlySemanticParameterNames(function *ast.FuncDecl) []string {
	if function.Type.Params == nil {
		return nil
	}
	var names []string
	for _, field := range function.Type.Params.List {
		if len(field.Names) == 0 {
			names = append(names, "")
			continue
		}
		for _, name := range field.Names {
			names = append(names, name.Name)
		}
	}
	return names
}

func forwardOnlySemanticCollectConstants(pkg *forwardOnlySemanticPackage, file *ast.File) {
	for _, declaration := range file.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok || general.Tok != token.CONST {
			continue
		}
		for _, spec := range general.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok || len(value.Names) != len(value.Values) {
				continue
			}
			for index, name := range value.Names {
				if resolved, ok := forwardOnlySemanticStringValue(pkg, value.Values[index]); ok {
					pkg.constants[name.Name] = resolved
				}
			}
		}
	}
}

func forwardOnlySemanticProductionViolations(pkg *forwardOnlySemanticPackage) []Violation {
	var violations []Violation
	for _, functions := range pkg.functions {
		for _, function := range functions {
			if function.file.test {
				continue
			}
			if forwardOnlySemanticDualWaitReader(pkg, function) {
				violations = append(violations, forwardOnlyViolation(function.file.set, function.file.path, function.decl.Name,
					"old and current parent-wait transports must not converge on the same accepted wait consumer"))
			}
			if forwardOnlySemanticNormalizesCurrentWait(pkg, function) {
				violations = append(violations, forwardOnlyViolation(function.file.set, function.file.path, function.decl.Name,
					"current parent-wait transport must not be rewritten into the old function_call/wait representation"))
			}
		}
	}
	return violations
}

func forwardOnlySemanticDualWaitReader(pkg *forwardOnlySemanticPackage, function *forwardOnlySemanticFunction) bool {
	var oldSinks map[string]bool
	var currentSinks map[string]bool
	for _, arm := range forwardOnlySemanticTransportArms(pkg, function.decl.Body) {
		markers := forwardOnlySemanticObjectWaitMarkers(pkg, arm.node, arm.object)
		sinks := forwardOnlySemanticSinks(pkg, function, arm.node, forwardOnlyTransportWalkLimit, nil)
		if arm.kind&forwardOnlyOldTransport != 0 && markers&forwardOnlyOldWait != 0 {
			oldSinks = mergeForwardOnlySemanticSet(oldSinks, sinks)
		}
		if arm.kind&forwardOnlyCurrentTransport != 0 && markers&forwardOnlyCurrentWait != 0 {
			currentSinks = mergeForwardOnlySemanticSet(currentSinks, sinks)
		}
	}
	return forwardOnlySemanticSetsIntersect(oldSinks, currentSinks)
}

func forwardOnlySemanticTransportArms(pkg *forwardOnlySemanticPackage, body *ast.BlockStmt) []forwardOnlyTransportArm {
	var arms []forwardOnlyTransportArm
	ast.Inspect(body, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.SwitchStmt:
			object := forwardOnlySemanticTypedObject(typed.Tag)
			if object == "" {
				return true
			}
			for _, statement := range typed.Body.List {
				clause, ok := statement.(*ast.CaseClause)
				if !ok {
					continue
				}
				kind := forwardOnlySemanticTransportExpressions(pkg, clause.List)
				if kind != 0 {
					arms = append(arms, forwardOnlyTransportArm{kind: kind, object: object, node: &ast.BlockStmt{List: clause.Body}})
				}
			}
		case *ast.IfStmt:
			kind, object := forwardOnlySemanticConditionTransport(pkg, typed.Cond)
			if kind != 0 && object != "" {
				arms = append(arms, forwardOnlyTransportArm{kind: kind, object: object, node: typed.Body})
			}
		}
		return true
	})
	return arms
}

func forwardOnlySemanticTypedObject(expression ast.Expr) string {
	selector, ok := forwardOnlyUnparen(expression).(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "Type" {
		return ""
	}
	return forwardOnlySemanticObjectBase(selector.X)
}

func forwardOnlySemanticTransportExpressions(pkg *forwardOnlySemanticPackage, expressions []ast.Expr) uint8 {
	var kind uint8
	for _, expression := range expressions {
		kind |= forwardOnlySemanticTransportExpression(pkg, expression)
	}
	return kind
}

func forwardOnlySemanticConditionTransport(pkg *forwardOnlySemanticPackage, expression ast.Expr) (uint8, string) {
	binary, ok := forwardOnlyUnparen(expression).(*ast.BinaryExpr)
	if !ok {
		return 0, ""
	}
	if binary.Op == token.LAND || binary.Op == token.LOR {
		leftKind, leftObject := forwardOnlySemanticConditionTransport(pkg, binary.X)
		rightKind, rightObject := forwardOnlySemanticConditionTransport(pkg, binary.Y)
		if leftObject != "" {
			return leftKind | rightKind, leftObject
		}
		return leftKind | rightKind, rightObject
	}
	if binary.Op != token.EQL {
		return 0, ""
	}
	if object := forwardOnlySemanticTypedObject(binary.X); object != "" {
		return forwardOnlySemanticTransportExpression(pkg, binary.Y), object
	}
	if object := forwardOnlySemanticTypedObject(binary.Y); object != "" {
		return forwardOnlySemanticTransportExpression(pkg, binary.X), object
	}
	return 0, ""
}

func forwardOnlySemanticTransportExpression(pkg *forwardOnlySemanticPackage, expression ast.Expr) uint8 {
	value, ok := forwardOnlySemanticStringValue(pkg, expression)
	if !ok {
		return 0
	}
	switch value {
	case "function_call":
		return forwardOnlyOldTransport
	case "custom_tool_call":
		return forwardOnlyCurrentTransport
	default:
		return 0
	}
}

func forwardOnlySemanticObjectWaitMarkers(pkg *forwardOnlySemanticPackage, node ast.Node, object string) uint8 {
	if object == "" {
		return 0
	}
	oldName := false
	currentName := false
	currentInput := false
	ast.Inspect(node, func(current ast.Node) bool {
		switch typed := current.(type) {
		case *ast.BinaryExpr:
			old, currentWaitName, input := forwardOnlySemanticObjectWaitComparison(pkg, typed, object)
			oldName = oldName || old
			currentName = currentName || currentWaitName
			currentInput = currentInput || input
		case *ast.SwitchStmt:
			old, currentWaitName := forwardOnlySemanticObjectNameSwitch(pkg, typed, object)
			oldName = oldName || old
			currentName = currentName || currentWaitName
		case *ast.CallExpr:
			currentInput = currentInput || forwardOnlySemanticCurrentInputCall(pkg, typed, object)
		}
		return true
	})
	var markers uint8
	if oldName {
		markers |= forwardOnlyOldWait
	}
	if currentName && currentInput {
		markers |= forwardOnlyCurrentWait
	}
	return markers
}

func forwardOnlySemanticObjectWaitComparison(pkg *forwardOnlySemanticPackage, expression *ast.BinaryExpr, object string) (bool, bool, bool) {
	if expression.Op != token.EQL && expression.Op != token.NEQ {
		return false, false, false
	}
	if forwardOnlySemanticObjectField(expression.X, object, "Name") {
		return forwardOnlySemanticWaitNameValue(pkg, expression.Y)
	}
	if forwardOnlySemanticObjectField(expression.Y, object, "Name") {
		return forwardOnlySemanticWaitNameValue(pkg, expression.X)
	}
	if forwardOnlySemanticObjectField(expression.X, object, "Input") {
		return false, false, forwardOnlySemanticCurrentInputValue(pkg, expression.Y)
	}
	if forwardOnlySemanticObjectField(expression.Y, object, "Input") {
		return false, false, forwardOnlySemanticCurrentInputValue(pkg, expression.X)
	}
	return false, false, false
}

func forwardOnlySemanticWaitNameValue(pkg *forwardOnlySemanticPackage, expression ast.Expr) (bool, bool, bool) {
	value, ok := forwardOnlySemanticStringValue(pkg, expression)
	if !ok {
		return false, false, false
	}
	switch value {
	case "wait":
		return true, false, false
	case "exec":
		return false, true, false
	default:
		return false, false, false
	}
}

func forwardOnlySemanticObjectNameSwitch(pkg *forwardOnlySemanticPackage, statement *ast.SwitchStmt, object string) (bool, bool) {
	if !forwardOnlySemanticObjectField(statement.Tag, object, "Name") {
		return false, false
	}
	oldName := false
	currentName := false
	for _, item := range statement.Body.List {
		clause, ok := item.(*ast.CaseClause)
		if !ok {
			continue
		}
		for _, expression := range clause.List {
			old, currentWaitName, _ := forwardOnlySemanticWaitNameValue(pkg, expression)
			oldName = oldName || old
			currentName = currentName || currentWaitName
		}
	}
	return oldName, currentName
}

func forwardOnlySemanticCurrentInputCall(pkg *forwardOnlySemanticPackage, call *ast.CallExpr, object string) bool {
	if identifier, ok := forwardOnlyUnparen(call.Fun).(*ast.Ident); ok && identifier.Name == "analysisCustomWaitRequestedYield" {
		for _, argument := range call.Args {
			if forwardOnlySemanticObjectField(argument, object, "Input") {
				return true
			}
		}
	}
	hasInput := false
	hasWriteStdin := false
	for _, argument := range call.Args {
		hasInput = hasInput || forwardOnlySemanticObjectField(argument, object, "Input")
		hasWriteStdin = hasWriteStdin || forwardOnlySemanticCurrentInputValue(pkg, argument)
	}
	return hasInput && hasWriteStdin
}

func forwardOnlySemanticCurrentInputValue(pkg *forwardOnlySemanticPackage, expression ast.Expr) bool {
	value, ok := forwardOnlySemanticStringValue(pkg, expression)
	return ok && strings.Contains(value, "tools.write_stdin")
}

func forwardOnlySemanticObjectField(expression ast.Expr, object, field string) bool {
	selector, ok := forwardOnlyUnparen(expression).(*ast.SelectorExpr)
	return ok && selector.Sel.Name == field && forwardOnlySemanticObjectBase(selector.X) == object
}

func forwardOnlySemanticSinks(pkg *forwardOnlySemanticPackage, function *forwardOnlySemanticFunction, node ast.Node, remaining int, seen map[*forwardOnlySemanticFunction]bool) map[string]bool {
	sinks := forwardOnlySemanticDirectSinks(node)
	if remaining == 0 {
		return sinks
	}
	for _, call := range forwardOnlySemanticCalls(node) {
		callee := forwardOnlySemanticResolveCall(pkg, function.file, call)
		if callee == nil || callee.file.test {
			continue
		}
		if seen == nil {
			seen = make(map[*forwardOnlySemanticFunction]bool)
		}
		if seen[callee] {
			continue
		}
		seen[callee] = true
		mergeForwardOnlySemanticSet(sinks, forwardOnlySemanticSinks(pkg, callee, callee.decl.Body, remaining-1, seen))
		delete(seen, callee)
	}
	return sinks
}

func forwardOnlySemanticDirectSinks(node ast.Node) map[string]bool {
	sinks := make(map[string]bool)
	ast.Inspect(node, func(current ast.Node) bool {
		switch typed := current.(type) {
		case *ast.CallExpr:
			identifier, ok := forwardOnlyUnparen(typed.Fun).(*ast.Ident)
			if ok && identifier.Name == "append" && len(typed.Args) > 0 {
				sinks["append:"+forwardOnlySemanticExpressionKey(typed.Args[0])] = true
			}
		case *ast.ReturnStmt:
			for _, result := range typed.Results {
				if identifier, ok := forwardOnlyUnparen(result).(*ast.Ident); ok && identifier.Name == "true" {
					sinks["return:true"] = true
				}
			}
		}
		return true
	})
	return sinks
}

func forwardOnlySemanticNormalizesCurrentWait(pkg *forwardOnlySemanticPackage, function *forwardOnlySemanticFunction) bool {
	for _, arm := range forwardOnlySemanticTransportArms(pkg, function.decl.Body) {
		if arm.kind&forwardOnlyCurrentTransport == 0 {
			continue
		}
		if forwardOnlySemanticObjectWaitMarkers(pkg, arm.node, arm.object)&forwardOnlyCurrentWait == 0 {
			continue
		}
		if forwardOnlySemanticArmNormalizesObject(pkg, function, arm, forwardOnlyTransportWalkLimit) {
			return true
		}
	}
	return false
}

func forwardOnlySemanticArmNormalizesObject(pkg *forwardOnlySemanticPackage, function *forwardOnlySemanticFunction, arm forwardOnlyTransportArm, remaining int) bool {
	if forwardOnlySemanticOldWaitRewriteObjects(pkg, arm.node)[arm.object] {
		return true
	}
	for _, call := range forwardOnlySemanticCalls(arm.node) {
		callee := forwardOnlySemanticResolveCall(pkg, function.file, call)
		if callee == nil || callee.file.test {
			continue
		}
		normalized := forwardOnlySemanticNormalizedParameters(pkg, callee, remaining, nil)
		for index := range normalized {
			if index < len(call.Args) && forwardOnlySemanticObjectBase(call.Args[index]) == arm.object {
				return true
			}
		}
	}
	return false
}

func forwardOnlySemanticNormalizedParameters(pkg *forwardOnlySemanticPackage, function *forwardOnlySemanticFunction, remaining int, seen map[*forwardOnlySemanticFunction]bool) map[int]bool {
	result := forwardOnlySemanticDirectNormalizedParameters(pkg, function)
	if remaining == 0 {
		return result
	}
	if seen == nil {
		seen = make(map[*forwardOnlySemanticFunction]bool)
	}
	if seen[function] {
		return result
	}
	seen[function] = true
	defer delete(seen, function)
	for _, call := range forwardOnlySemanticCalls(function.decl.Body) {
		callee := forwardOnlySemanticResolveCall(pkg, function.file, call)
		if callee == nil || callee.file.test {
			continue
		}
		for index := range forwardOnlySemanticNormalizedParameters(pkg, callee, remaining-1, seen) {
			if index >= len(call.Args) {
				continue
			}
			argument := forwardOnlySemanticObjectBase(call.Args[index])
			if parameter := forwardOnlySemanticParameterIndex(function, argument); parameter >= 0 {
				result[parameter] = true
			}
		}
	}
	return result
}

func forwardOnlySemanticDirectNormalizedParameters(pkg *forwardOnlySemanticPackage, function *forwardOnlySemanticFunction) map[int]bool {
	result := make(map[int]bool)
	for object := range forwardOnlySemanticOldWaitRewriteObjects(pkg, function.decl.Body) {
		if index := forwardOnlySemanticParameterIndex(function, object); index >= 0 {
			result[index] = true
		}
	}
	return result
}

func forwardOnlySemanticParameterIndex(function *forwardOnlySemanticFunction, name string) int {
	for index, parameter := range function.params {
		if name != "" && parameter == name {
			return index
		}
	}
	return -1
}

func forwardOnlySemanticOldWaitRewriteObjects(pkg *forwardOnlySemanticPackage, node ast.Node) map[string]bool {
	writes := make(map[string]uint8)
	ast.Inspect(node, func(current ast.Node) bool {
		assignment, ok := current.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for index, target := range assignment.Lhs {
			if index >= len(assignment.Rhs) {
				continue
			}
			selector, ok := forwardOnlyUnparen(target).(*ast.SelectorExpr)
			if !ok {
				continue
			}
			object := forwardOnlySemanticObjectBase(selector.X)
			if object == "" {
				continue
			}
			switch selector.Sel.Name {
			case "Type":
				if forwardOnlySemanticTransportExpression(pkg, assignment.Rhs[index])&forwardOnlyOldTransport != 0 {
					writes[object] |= forwardOnlyOldTransport
				}
			case "Name":
				if value, ok := forwardOnlySemanticStringValue(pkg, assignment.Rhs[index]); ok && value == "wait" {
					writes[object] |= forwardOnlyOldWait
				}
			}
		}
		return true
	})
	result := make(map[string]bool)
	for object, flags := range writes {
		if flags&(forwardOnlyOldTransport|forwardOnlyOldWait) == forwardOnlyOldTransport|forwardOnlyOldWait {
			result[object] = true
		}
	}
	return result
}

func forwardOnlySemanticTestViolations(pkg *forwardOnlySemanticPackage) []Violation {
	var violations []Violation
	for _, functions := range pkg.functions {
		for _, function := range functions {
			if !function.file.test {
				continue
			}
			accepted := forwardOnlySemanticAcceptedTestTransports(pkg, function.decl.Body)
			if accepted&forwardOnlyOldTransport != 0 && accepted&forwardOnlyCurrentTransport != 0 {
				violations = append(violations, forwardOnlyViolation(function.file.set, function.file.path, function.decl.Name,
					"tests must not guarantee successful acceptance of both old and current parent-wait transports"))
			}
		}
	}
	return violations
}

func forwardOnlySemanticAcceptedTestTransports(pkg *forwardOnlySemanticPackage, body *ast.BlockStmt) uint8 {
	var accepted uint8
	ast.Inspect(body, func(node ast.Node) bool {
		statement, ok := node.(*ast.IfStmt)
		if !ok || !forwardOnlySemanticFailureBlock(statement.Body) {
			return true
		}
		call := forwardOnlySemanticExpectedTrueCall(statement)
		if call != nil {
			accepted |= forwardOnlySemanticCallWaitTransport(pkg, call)
		}
		return true
	})
	return accepted
}

func forwardOnlySemanticFailureBlock(body *ast.BlockStmt) bool {
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
		}
		return true
	})
	return failed
}

func forwardOnlySemanticExpectedTrueCall(statement *ast.IfStmt) *ast.CallExpr {
	if call := forwardOnlySemanticNegatedCall(statement.Cond); call != nil {
		return call
	}
	assignment, ok := statement.Init.(*ast.AssignStmt)
	if !ok || len(assignment.Lhs) != 1 || len(assignment.Rhs) != 1 {
		return nil
	}
	name, ok := forwardOnlyUnparen(assignment.Lhs[0]).(*ast.Ident)
	if !ok {
		return nil
	}
	call, ok := forwardOnlyUnparen(assignment.Rhs[0]).(*ast.CallExpr)
	if !ok || !forwardOnlySemanticConditionMeansFalse(statement.Cond, name.Name) {
		return nil
	}
	return call
}

func forwardOnlySemanticNegatedCall(expression ast.Expr) *ast.CallExpr {
	switch typed := forwardOnlyUnparen(expression).(type) {
	case *ast.UnaryExpr:
		if typed.Op != token.NOT {
			return nil
		}
		call, _ := forwardOnlyUnparen(typed.X).(*ast.CallExpr)
		return call
	case *ast.BinaryExpr:
		if call, ok := forwardOnlyUnparen(typed.X).(*ast.CallExpr); ok && forwardOnlySemanticBooleanFailure(typed.Op, typed.Y) {
			return call
		}
		if call, ok := forwardOnlyUnparen(typed.Y).(*ast.CallExpr); ok && forwardOnlySemanticBooleanFailure(typed.Op, typed.X) {
			return call
		}
	}
	return nil
}

func forwardOnlySemanticConditionMeansFalse(expression ast.Expr, name string) bool {
	switch typed := forwardOnlyUnparen(expression).(type) {
	case *ast.UnaryExpr:
		identifier, ok := forwardOnlyUnparen(typed.X).(*ast.Ident)
		return typed.Op == token.NOT && ok && identifier.Name == name
	case *ast.BinaryExpr:
		identifier, ok := forwardOnlyUnparen(typed.X).(*ast.Ident)
		if ok && identifier.Name == name {
			return forwardOnlySemanticBooleanFailure(typed.Op, typed.Y)
		}
		identifier, ok = forwardOnlyUnparen(typed.Y).(*ast.Ident)
		return ok && identifier.Name == name && forwardOnlySemanticBooleanFailure(typed.Op, typed.X)
	default:
		return false
	}
}

func forwardOnlySemanticBooleanFailure(operator token.Token, expression ast.Expr) bool {
	identifier, ok := forwardOnlyUnparen(expression).(*ast.Ident)
	if !ok {
		return false
	}
	return operator == token.EQL && identifier.Name == "false" || operator == token.NEQ && identifier.Name == "true"
}

func forwardOnlySemanticCallWaitTransport(pkg *forwardOnlySemanticPackage, call *ast.CallExpr) uint8 {
	var result uint8
	for _, argument := range call.Args {
		ast.Inspect(argument, func(node ast.Node) bool {
			literal, ok := node.(*ast.CompositeLit)
			if ok {
				result |= forwardOnlySemanticCompositeWaitTransport(pkg, literal)
			}
			return true
		})
	}
	return result
}

func forwardOnlySemanticCompositeWaitTransport(pkg *forwardOnlySemanticPackage, literal *ast.CompositeLit) uint8 {
	fields := forwardOnlySemanticCompositeFields(pkg, literal)
	transport := fields["Type"]
	if transport == "" {
		transport = fields["type"]
	}
	name := fields["Name"]
	if name == "" {
		name = fields["name"]
	}
	input := fields["Input"]
	if input == "" {
		input = fields["input"]
	}
	switch transport {
	case "function_call":
		if name == "wait" {
			return forwardOnlyOldTransport
		}
	case "custom_tool_call":
		if name == "exec" && strings.Contains(input, "tools.write_stdin") {
			return forwardOnlyCurrentTransport
		}
	}
	return 0
}

func forwardOnlySemanticCompositeFields(pkg *forwardOnlySemanticPackage, literal *ast.CompositeLit) map[string]string {
	fields := make(map[string]string)
	for _, element := range literal.Elts {
		pair, ok := element.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key := forwardOnlySemanticFieldKey(pair.Key)
		if key == "" {
			continue
		}
		if value, ok := forwardOnlySemanticStringValue(pkg, pair.Value); ok {
			fields[key] = value
		}
	}
	return fields
}

func forwardOnlySemanticFieldKey(expression ast.Expr) string {
	switch typed := forwardOnlyUnparen(expression).(type) {
	case *ast.Ident:
		return typed.Name
	case *ast.BasicLit:
		if typed.Kind == token.STRING {
			value, _ := strconv.Unquote(typed.Value)
			return value
		}
	}
	return ""
}

func forwardOnlySemanticCalls(node ast.Node) []*ast.CallExpr {
	var calls []*ast.CallExpr
	ast.Inspect(node, func(current ast.Node) bool {
		if call, ok := current.(*ast.CallExpr); ok {
			calls = append(calls, call)
		}
		return true
	})
	return calls
}

func forwardOnlySemanticResolveCall(pkg *forwardOnlySemanticPackage, file *forwardOnlySemanticFile, call *ast.CallExpr) *forwardOnlySemanticFunction {
	var name string
	method := false
	switch typed := forwardOnlyUnparen(call.Fun).(type) {
	case *ast.Ident:
		name = typed.Name
	case *ast.SelectorExpr:
		identifier, ok := forwardOnlyUnparen(typed.X).(*ast.Ident)
		if !ok || file.imports[identifier.Name] {
			return nil
		}
		name = typed.Sel.Name
		method = true
	default:
		return nil
	}
	var candidates []*forwardOnlySemanticFunction
	for _, candidate := range pkg.functions[name] {
		if candidate.file.test != file.test {
			continue
		}
		if method != (candidate.decl.Recv != nil) {
			continue
		}
		candidates = append(candidates, candidate)
	}
	if len(candidates) != 1 {
		return nil
	}
	return candidates[0]
}

func forwardOnlySemanticObjectBase(expression ast.Expr) string {
	switch typed := forwardOnlyUnparen(expression).(type) {
	case *ast.Ident:
		return typed.Name
	case *ast.StarExpr:
		return forwardOnlySemanticObjectBase(typed.X)
	case *ast.SelectorExpr:
		base := forwardOnlySemanticObjectBase(typed.X)
		if base != "" {
			return base + "." + typed.Sel.Name
		}
	}
	return ""
}

func forwardOnlySemanticExpressionKey(expression ast.Expr) string {
	switch typed := forwardOnlyUnparen(expression).(type) {
	case *ast.Ident:
		return typed.Name
	case *ast.SelectorExpr:
		base := forwardOnlySemanticExpressionKey(typed.X)
		if base != "" {
			return base + "." + typed.Sel.Name
		}
	}
	return ""
}

func forwardOnlySemanticStringValue(pkg *forwardOnlySemanticPackage, expression ast.Expr) (string, bool) {
	switch typed := forwardOnlyUnparen(expression).(type) {
	case *ast.BasicLit:
		if typed.Kind != token.STRING {
			return "", false
		}
		value, err := strconv.Unquote(typed.Value)
		return value, err == nil
	case *ast.Ident:
		value, ok := pkg.constants[typed.Name]
		return value, ok
	case *ast.BinaryExpr:
		if typed.Op != token.ADD {
			return "", false
		}
		left, leftOK := forwardOnlySemanticStringValue(pkg, typed.X)
		right, rightOK := forwardOnlySemanticStringValue(pkg, typed.Y)
		if !leftOK || !rightOK {
			return "", false
		}
		return left + right, true
	default:
		return "", false
	}
}

func mergeForwardOnlySemanticSet(target, source map[string]bool) map[string]bool {
	if target == nil {
		target = make(map[string]bool)
	}
	for value := range source {
		target[value] = true
	}
	return target
}

func forwardOnlySemanticSetsIntersect(left, right map[string]bool) bool {
	for value := range left {
		if right[value] {
			return true
		}
	}
	return false
}
