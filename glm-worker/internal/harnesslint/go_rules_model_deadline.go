package harnesslint

import (
	"go/ast"
	"go/token"
	"strings"
)

type modelCommandSite struct {
	name string
	pos  token.Pos
}

type modelDeadlineChecker struct {
	set        *token.FileSet
	path       string
	commands   []modelCommandSite
	assigned   map[token.Pos]string
	creations  map[token.Pos]bool
	bounded    map[string]bool
	violations []Violation
}

const modelDeadlineRule = "model-invocation-deadline"

const modelDeadlineScopePrefix = "glm-worker/internal/runner/"

const modelDeadlineConstructor = "newProcessGroupCmd"

const modelDeadlineBinaryField = "ClaudeBin"

const modelDeadlineStopSink = "runCommand"

const modelDeadlineDeadlineSink = "runProbeCommand"

const modelDeadlineProbeAPI = "ProbeWithDeadline"

var modelDeadlineRawExecutors = map[string]bool{
	"Run":   true,
	"Start": true,
	"Wait":  true,
}

func modelDeadlineViolations(set *token.FileSet, file *ast.File, path string, data []byte) []Violation {
	if !strings.HasPrefix(path, modelDeadlineScopePrefix) || strings.HasSuffix(path, "_test.go") || hasBuildConstraint(data) {
		return nil
	}
	var violations []Violation
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Body == nil {
			continue
		}
		violations = append(violations, modelDeadlineFunctionViolations(set, path, function)...)
	}
	return violations
}

func modelDeadlineFunctionViolations(set *token.FileSet, path string, function *ast.FuncDecl) []Violation {
	checker := &modelDeadlineChecker{
		set:       set,
		path:      path,
		assigned:  map[token.Pos]string{},
		creations: map[token.Pos]bool{},
		bounded:   map[string]bool{},
	}
	checker.collect(function)
	checker.inspectUsages(function)
	checker.reportUnbounded()
	return checker.violations
}

func (c *modelDeadlineChecker) collect(function *ast.FuncDecl) {
	ast.Inspect(function.Body, c.recordCommandCreations)
	ast.Inspect(function.Body, c.recordCommandAssignments)
}

func (c *modelDeadlineChecker) recordCommandCreations(node ast.Node) bool {
	call, ok := node.(*ast.CallExpr)
	if !ok || !modelDeadlineCreatesCommand(call) {
		return true
	}
	c.creations[call.Pos()] = true
	c.commands = append(c.commands, modelCommandSite{pos: call.Pos()})
	return true
}

func (c *modelDeadlineChecker) recordCommandAssignments(node ast.Node) bool {
	assignment, ok := node.(*ast.AssignStmt)
	if !ok || len(assignment.Lhs) != len(assignment.Rhs) {
		return true
	}
	for index, value := range assignment.Rhs {
		c.recordCommandAssignment(assignment.Lhs[index], value)
	}
	return true
}

func (c *modelDeadlineChecker) recordCommandAssignment(target, value ast.Expr) {
	call, ok := value.(*ast.CallExpr)
	if !ok || !c.creations[call.Pos()] {
		return
	}
	identifier, ok := target.(*ast.Ident)
	if !ok {
		return
	}
	c.assigned[call.Pos()] = identifier.Name
	c.nameCreationSite(call.Pos(), identifier.Name)
}

func (c *modelDeadlineChecker) nameCreationSite(position token.Pos, name string) {
	for index, site := range c.commands {
		if site.pos == position {
			c.commands[index].name = name
		}
	}
}

func (c *modelDeadlineChecker) inspectUsages(function *ast.FuncDecl) {
	names := c.commandNames()
	ast.Inspect(function.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		c.inspectUsageCall(call, names)
		return true
	})
}

func (c *modelDeadlineChecker) inspectUsageCall(call *ast.CallExpr, names map[string]bool) {
	if c.recordRawExecution(call, names) || c.recordZeroDeadlineDelegation(call) {
		return
	}
	sink, arguments := modelDeadlineSinkCall(call)
	if sink == "" {
		return
	}
	for _, argument := range arguments {
		c.recordSinkArgument(call, sink, arguments, argument)
	}
}

func (c *modelDeadlineChecker) recordRawExecution(call *ast.CallExpr, names map[string]bool) bool {
	if !modelDeadlineRawCommandCall(call, names) {
		return false
	}
	c.addViolation(call.Pos(), "model-binary command must not use a raw process execution call; route it through "+modelDeadlineStopSink+" or "+modelDeadlineDeadlineSink)
	return true
}

func (c *modelDeadlineChecker) recordZeroDeadlineDelegation(call *ast.CallExpr) bool {
	if !modelDeadlineZeroDeadlineDelegation(call) {
		return false
	}
	c.addViolation(call.Pos(), modelDeadlineProbeAPI+" must receive a non-zero deadline instead of delegating an unbounded zero deadline")
	return true
}

func (c *modelDeadlineChecker) recordSinkArgument(call *ast.CallExpr, sink string, arguments []ast.Expr, argument ast.Expr) {
	name, ok := argument.(*ast.Ident)
	if !ok || !c.tracks(name.Name) {
		return
	}
	if sink == modelDeadlineDeadlineSink && modelDeadlineZeroTimeLiteral(modelDeadlineDeadlineArgument(arguments)) {
		c.addViolation(call.Pos(), "model-binary command needs a non-zero deadline when executed through "+modelDeadlineDeadlineSink)
		return
	}
	c.bounded[name.Name] = true
}

func (c *modelDeadlineChecker) reportUnbounded() {
	for _, site := range c.commands {
		if assigned, ok := c.assigned[site.pos]; ok {
			if !c.bounded[assigned] {
				c.addViolation(site.pos, "model-binary command must be executed through "+modelDeadlineStopSink+" or "+modelDeadlineDeadlineSink+" with a deadline")
			}
			continue
		}
		c.addViolation(site.pos, "model-binary command must be assigned to a local variable and executed through "+modelDeadlineStopSink+" or "+modelDeadlineDeadlineSink+" with a deadline")
	}
}

func (c *modelDeadlineChecker) commandNames() map[string]bool {
	names := make(map[string]bool, len(c.commands))
	for _, site := range c.commands {
		if site.name != "" {
			names[site.name] = true
		}
	}
	return names
}

func (c *modelDeadlineChecker) tracks(name string) bool {
	for _, site := range c.commands {
		if site.name == name {
			return true
		}
	}
	return false
}

func (c *modelDeadlineChecker) addViolation(position token.Pos, message string) {
	location := c.set.Position(position)
	c.violations = append(c.violations, Violation{
		Rule: modelDeadlineRule, Path: c.path, Line: location.Line, Column: location.Column,
		Message: message,
	})
}

func modelDeadlineCreatesCommand(call *ast.CallExpr) bool {
	identifier, ok := call.Fun.(*ast.Ident)
	if !ok || identifier.Name != modelDeadlineConstructor || len(call.Args) == 0 {
		return false
	}
	return modelDeadlineReferencesBinaryField(call.Args[0])
}

func modelDeadlineReferencesBinaryField(expression ast.Expr) bool {
	found := false
	ast.Inspect(expression, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if ok && selector.Sel.Name == modelDeadlineBinaryField {
			found = true
		}
		return !found
	})
	return found
}

func modelDeadlineRawCommandCall(call *ast.CallExpr, names map[string]bool) bool {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	identifier, ok := selector.X.(*ast.Ident)
	return ok && names[identifier.Name] && modelDeadlineRawExecutors[selector.Sel.Name]
}

func modelDeadlineZeroDeadlineDelegation(call *ast.CallExpr) bool {
	var name string
	switch typed := call.Fun.(type) {
	case *ast.Ident:
		name = typed.Name
	case *ast.SelectorExpr:
		name = typed.Sel.Name
	}
	if name != modelDeadlineProbeAPI || len(call.Args) == 0 {
		return false
	}
	return modelDeadlineZeroTimeLiteral(call.Args[len(call.Args)-1])
}

func modelDeadlineSinkCall(call *ast.CallExpr) (string, []ast.Expr) {
	switch typed := call.Fun.(type) {
	case *ast.Ident:
		if typed.Name == modelDeadlineStopSink || typed.Name == modelDeadlineDeadlineSink {
			return typed.Name, call.Args
		}
	case *ast.SelectorExpr:
		if typed.Sel.Name == modelDeadlineStopSink || typed.Sel.Name == modelDeadlineDeadlineSink {
			return typed.Sel.Name, call.Args
		}
	}
	return "", nil
}

func modelDeadlineDeadlineArgument(arguments []ast.Expr) ast.Expr {
	if len(arguments) < 2 {
		return nil
	}
	return forwardOnlyUnparen(arguments[1])
}

func modelDeadlineZeroTimeLiteral(expression ast.Expr) bool {
	literal, ok := forwardOnlyUnparen(expression).(*ast.CompositeLit)
	if !ok {
		return false
	}
	selector, ok := literal.Type.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	packageIdentifier, ok := selector.X.(*ast.Ident)
	return ok && packageIdentifier.Name == "time" && selector.Sel.Name == "Time"
}
