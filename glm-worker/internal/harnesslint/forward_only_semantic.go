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
	file *forwardOnlySemanticFile
	decl *ast.FuncDecl
	name string
}

type forwardOnlySemanticPackage struct {
	constants map[string]string
	functions map[string][]*forwardOnlySemanticFunction
	files     []*forwardOnlySemanticFile
}

type forwardOnlyTransportArm struct {
	kind    uint8
	node    ast.Node
	markers uint8
}

const (
	forwardOnlyOldTransport uint8 = 1 << iota
	forwardOnlyCurrentTransport
	forwardOnlyOldWait
	forwardOnlyCurrentWait
	forwardOnlyTransportWalkLimit = 4
)

const forwardOnlyOldWaitName = "wait"

const forwardOnlyCurrentWaitInput = "tools.write_stdin"

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
			indexed := &forwardOnlySemanticFunction{file: file, decl: function, name: function.Name.Name}
			pkg.functions[indexed.name] = append(pkg.functions[indexed.name], indexed)
		}
	}
}

func forwardOnlySemanticCollectConstants(pkg *forwardOnlySemanticPackage, file *ast.File) {
	for _, declaration := range file.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok || general.Tok != token.CONST {
			continue
		}
		forwardOnlySemanticCollectConstantSpecs(pkg, general.Specs)
	}
}

func forwardOnlySemanticCollectConstantSpecs(pkg *forwardOnlySemanticPackage, specs []ast.Spec) {
	for _, spec := range specs {
		value, ok := spec.(*ast.ValueSpec)
		if !ok || len(value.Names) != len(value.Values) {
			continue
		}
		forwardOnlySemanticCollectConstantValues(pkg, value)
	}
}

func forwardOnlySemanticCollectConstantValues(pkg *forwardOnlySemanticPackage, value *ast.ValueSpec) {
	for index, name := range value.Names {
		resolved, ok := forwardOnlySemanticStringValue(pkg, value.Values[index])
		if ok {
			pkg.constants[name.Name] = resolved
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
	arms := forwardOnlySemanticTransportArms(pkg, function.decl.Body)
	var oldSinks, currentSinks map[string]bool
	for _, arm := range arms {
		markers := arm.markers | forwardOnlySemanticWaitMarkers(pkg, function, arm.node, forwardOnlyTransportWalkLimit, nil)
		sinks := forwardOnlySemanticSinks(pkg, function, arm.node, forwardOnlyTransportWalkLimit, nil)
		switch {
		case arm.kind == forwardOnlyOldTransport && markers&forwardOnlyOldWait != 0:
			oldSinks = mergeForwardOnlySemanticSet(oldSinks, sinks)
		case arm.kind == forwardOnlyCurrentTransport && markers&forwardOnlyCurrentWait != 0:
			currentSinks = mergeForwardOnlySemanticSet(currentSinks, sinks)
		}
	}
	return forwardOnlySemanticSetsIntersect(oldSinks, currentSinks)
}

func forwardOnlySemanticTransportArms(pkg *forwardOnlySemanticPackage, body *ast.BlockStmt) []forwardOnlyTransportArm {
	var arms []forwardOnlyTransportArm
	ast.Inspect(body, func(node ast.Node) bool {
		arms = append(arms, forwardOnlySemanticTransportNodeArms(pkg, node)...)
		return true
	})
	return arms
}

func forwardOnlySemanticTransportNodeArms(pkg *forwardOnlySemanticPackage, node ast.Node) []forwardOnlyTransportArm {
	switch typed := node.(type) {
	case *ast.SwitchStmt:
		return forwardOnlySemanticSwitchTransportArms(pkg, typed)
	case *ast.IfStmt:
		return forwardOnlySemanticIfTransportArms(pkg, typed)
	default:
		return nil
	}
}

func forwardOnlySemanticSwitchTransportArms(pkg *forwardOnlySemanticPackage, statement *ast.SwitchStmt) []forwardOnlyTransportArm {
	if !forwardOnlySemanticTypeExpression(statement.Tag) {
		return nil
	}
	var arms []forwardOnlyTransportArm
	for _, item := range statement.Body.List {
		clause, ok := item.(*ast.CaseClause)
		if !ok {
			continue
		}
		kind := forwardOnlySemanticTransportExpressions(pkg, clause.List)
		if kind != 0 {
			arms = append(arms, forwardOnlyTransportArm{kind: kind, node: &ast.BlockStmt{List: clause.Body}})
		}
	}
	return arms
}

func forwardOnlySemanticIfTransportArms(pkg *forwardOnlySemanticPackage, statement *ast.IfStmt) []forwardOnlyTransportArm {
	kind := forwardOnlySemanticConditionTransport(pkg, statement.Cond)
	if kind == 0 {
		return nil
	}
	return []forwardOnlyTransportArm{{
		kind:    kind,
		node:    statement.Body,
		markers: forwardOnlySemanticDirectWaitMarkers(pkg, statement.Cond),
	}}
}

func forwardOnlySemanticTypeExpression(expression ast.Expr) bool {
	switch typed := forwardOnlyUnparen(expression).(type) {
	case *ast.SelectorExpr:
		return typed.Sel.Name == "Type"
	case *ast.Ident:
		return strings.EqualFold(typed.Name, "type") || strings.HasSuffix(strings.ToLower(typed.Name), "type")
	default:
		return false
	}
}

func forwardOnlySemanticTransportExpressions(pkg *forwardOnlySemanticPackage, expressions []ast.Expr) uint8 {
	var kind uint8
	for _, expression := range expressions {
		kind |= forwardOnlySemanticTransportExpression(pkg, expression)
	}
	return kind
}

func forwardOnlySemanticConditionTransport(pkg *forwardOnlySemanticPackage, expression ast.Expr) uint8 {
	binary, ok := forwardOnlyUnparen(expression).(*ast.BinaryExpr)
	if !ok {
		return 0
	}
	if binary.Op == token.LAND || binary.Op == token.LOR {
		return forwardOnlySemanticConditionTransport(pkg, binary.X) | forwardOnlySemanticConditionTransport(pkg, binary.Y)
	}
	if binary.Op != token.EQL {
		return 0
	}
	if forwardOnlySemanticTypeExpression(binary.X) {
		return forwardOnlySemanticTransportExpression(pkg, binary.Y)
	}
	if forwardOnlySemanticTypeExpression(binary.Y) {
		return forwardOnlySemanticTransportExpression(pkg, binary.X)
	}
	return 0
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

func forwardOnlySemanticWaitMarkers(pkg *forwardOnlySemanticPackage, function *forwardOnlySemanticFunction, node ast.Node, remaining int, seen map[*forwardOnlySemanticFunction]bool) uint8 {
	markers := forwardOnlySemanticDirectWaitMarkers(pkg, node)
	if remaining == 0 {
		return markers
	}
	for _, call := range forwardOnlySemanticCalls(node) {
		callee := forwardOnlySemanticResolveCall(pkg, function.file, call)
		if callee == nil || callee.file.test {
			continue
		}
		markers |= forwardOnlySemanticFunctionWaitMarkers(pkg, callee, remaining-1, seen)
	}
	return markers
}

func forwardOnlySemanticFunctionWaitMarkers(pkg *forwardOnlySemanticPackage, function *forwardOnlySemanticFunction, remaining int, seen map[*forwardOnlySemanticFunction]bool) uint8 {
	if seen == nil {
		seen = make(map[*forwardOnlySemanticFunction]bool)
	}
	if seen[function] {
		return 0
	}
	seen[function] = true
	defer delete(seen, function)
	return forwardOnlySemanticWaitMarkers(pkg, function, function.decl.Body, remaining, seen)
}

func forwardOnlySemanticDirectWaitMarkers(pkg *forwardOnlySemanticPackage, node ast.Node) uint8 {
	var markers uint8
	ast.Inspect(node, func(current ast.Node) bool {
		switch typed := current.(type) {
		case *ast.BinaryExpr:
			markers |= forwardOnlySemanticWaitComparison(pkg, typed)
		case *ast.CallExpr:
			for _, argument := range typed.Args {
				if value, ok := forwardOnlySemanticStringValue(pkg, argument); ok && strings.Contains(value, forwardOnlyCurrentWaitInput) {
					markers |= forwardOnlyCurrentWait
				}
			}
		case *ast.CompositeLit:
			markers |= forwardOnlySemanticCompositeWaitMarkers(pkg, typed)
		}
		return true
	})
	return markers
}

func forwardOnlySemanticWaitComparison(pkg *forwardOnlySemanticPackage, expression *ast.BinaryExpr) uint8 {
	if expression.Op != token.EQL && expression.Op != token.NEQ {
		return 0
	}
	if marker := forwardOnlySemanticWaitFieldComparison(pkg, expression.X, expression.Y); marker != 0 {
		return marker
	}
	return forwardOnlySemanticWaitFieldComparison(pkg, expression.Y, expression.X)
}

func forwardOnlySemanticWaitFieldComparison(pkg *forwardOnlySemanticPackage, field, value ast.Expr) uint8 {
	resolved, ok := forwardOnlySemanticStringValue(pkg, value)
	if !ok {
		return 0
	}
	switch {
	case forwardOnlySemanticNamedField(field, "Name") && resolved == forwardOnlyOldWaitName:
		return forwardOnlyOldWait
	case forwardOnlySemanticNamedField(field, "Input") && strings.Contains(resolved, forwardOnlyCurrentWaitInput):
		return forwardOnlyCurrentWait
	default:
		return 0
	}
}

func forwardOnlySemanticNamedField(expression ast.Expr, name string) bool {
	selector, ok := forwardOnlyUnparen(expression).(*ast.SelectorExpr)
	return ok && selector.Sel.Name == name
}

func forwardOnlySemanticCompositeWaitMarkers(pkg *forwardOnlySemanticPackage, literal *ast.CompositeLit) uint8 {
	fields := forwardOnlySemanticCompositeFields(pkg, literal)
	var markers uint8
	if fields["Name"] == forwardOnlyOldWaitName || fields["name"] == forwardOnlyOldWaitName {
		markers |= forwardOnlyOldWait
	}
	if strings.Contains(fields["Input"], forwardOnlyCurrentWaitInput) || strings.Contains(fields["input"], forwardOnlyCurrentWaitInput) {
		markers |= forwardOnlyCurrentWait
	}
	return markers
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
		forwardOnlySemanticRecordSink(sinks, current)
		return true
	})
	return sinks
}

func forwardOnlySemanticRecordSink(sinks map[string]bool, node ast.Node) {
	switch typed := node.(type) {
	case *ast.CallExpr:
		if sink := forwardOnlySemanticAppendSink(typed); sink != "" {
			sinks[sink] = true
		}
	case *ast.ReturnStmt:
		if forwardOnlySemanticReturnsTrue(typed) {
			sinks["return:true"] = true
		}
	}
}

func forwardOnlySemanticAppendSink(call *ast.CallExpr) string {
	identifier, ok := forwardOnlyUnparen(call.Fun).(*ast.Ident)
	if !ok || identifier.Name != "append" || len(call.Args) == 0 {
		return ""
	}
	selector, ok := forwardOnlyUnparen(call.Args[0]).(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	return "append:" + selector.Sel.Name
}

func forwardOnlySemanticReturnsTrue(statement *ast.ReturnStmt) bool {
	for _, result := range statement.Results {
		identifier, ok := forwardOnlyUnparen(result).(*ast.Ident)
		if ok && identifier.Name == "true" {
			return true
		}
	}
	return false
}

func forwardOnlySemanticNormalizesCurrentWait(pkg *forwardOnlySemanticPackage, function *forwardOnlySemanticFunction) bool {
	for _, arm := range forwardOnlySemanticTransportArms(pkg, function.decl.Body) {
		if arm.kind != forwardOnlyCurrentTransport {
			continue
		}
		markers := arm.markers | forwardOnlySemanticWaitMarkers(pkg, function, arm.node, forwardOnlyTransportWalkLimit, nil)
		if markers&forwardOnlyCurrentWait == 0 {
			continue
		}
		if forwardOnlySemanticNodeHasOldWaitRewrite(pkg, function, arm.node, forwardOnlyTransportWalkLimit, nil) {
			return true
		}
	}
	return false
}

func forwardOnlySemanticNodeHasOldWaitRewrite(pkg *forwardOnlySemanticPackage, function *forwardOnlySemanticFunction, node ast.Node, remaining int, seen map[*forwardOnlySemanticFunction]bool) bool {
	if forwardOnlySemanticSameObjectOldWaitRewrite(pkg, node) {
		return true
	}
	if remaining == 0 {
		return false
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
		rewrites := forwardOnlySemanticNodeHasOldWaitRewrite(pkg, callee, callee.decl.Body, remaining-1, seen)
		delete(seen, callee)
		if rewrites {
			return true
		}
	}
	return false
}

func forwardOnlySemanticSameObjectOldWaitRewrite(pkg *forwardOnlySemanticPackage, node ast.Node) bool {
	writes := make(map[string]uint8)
	ast.Inspect(node, func(current ast.Node) bool {
		assignment, ok := current.(*ast.AssignStmt)
		if ok {
			forwardOnlySemanticRecordRewriteAssignment(pkg, writes, assignment)
		}
		return true
	})
	return forwardOnlySemanticHasOldWaitRewrite(writes)
}

func forwardOnlySemanticRecordRewriteAssignment(pkg *forwardOnlySemanticPackage, writes map[string]uint8, assignment *ast.AssignStmt) {
	for index, target := range assignment.Lhs {
		if index >= len(assignment.Rhs) {
			continue
		}
		base, flag := forwardOnlySemanticRewriteTargetFlag(pkg, target, assignment.Rhs[index])
		if base != "" && flag != 0 {
			writes[base] |= flag
		}
	}
}

func forwardOnlySemanticRewriteTargetFlag(pkg *forwardOnlySemanticPackage, target, value ast.Expr) (string, uint8) {
	selector, ok := forwardOnlyUnparen(target).(*ast.SelectorExpr)
	if !ok {
		return "", 0
	}
	base := forwardOnlySemanticObjectBase(selector.X)
	if base == "" {
		return "", 0
	}
	switch selector.Sel.Name {
	case "Type":
		if forwardOnlySemanticTransportExpression(pkg, value) == forwardOnlyOldTransport {
			return base, forwardOnlyOldTransport
		}
	case "Name":
		resolved, ok := forwardOnlySemanticStringValue(pkg, value)
		if ok && resolved == forwardOnlyOldWaitName {
			return base, forwardOnlyOldWait
		}
	}
	return "", 0
}

func forwardOnlySemanticHasOldWaitRewrite(writes map[string]uint8) bool {
	for _, flags := range writes {
		if flags&(forwardOnlyOldTransport|forwardOnlyOldWait) == forwardOnlyOldTransport|forwardOnlyOldWait {
			return true
		}
	}
	return false
}

func forwardOnlySemanticObjectBase(expression ast.Expr) string {
	switch typed := forwardOnlyUnparen(expression).(type) {
	case *ast.Ident:
		return typed.Name
	case *ast.StarExpr:
		if identifier, ok := forwardOnlyUnparen(typed.X).(*ast.Ident); ok {
			return identifier.Name
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
		isMethod := candidate.decl.Recv != nil
		if method != isMethod {
			continue
		}
		candidates = append(candidates, candidate)
	}
	if len(candidates) != 1 {
		return nil
	}
	return candidates[0]
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
