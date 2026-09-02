package harnesslint

import (
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

type mainCallKind int

type mainPosition struct {
	terminal   bool
	errorIdent string
}

type mainBodyChecker struct {
	set          *token.FileSet
	path         string
	delegated    map[string]bool
	errorIdents  map[string]bool
	startupCalls int
}

const (
	mainCallRejected mainCallKind = iota
	mainCallStartup
	mainCallTerminalHelper
	mainCallExit
	mainCallStderrPrint
)

const mainFunctionName = "main"

const (
	fmtPackageName = "fmt"
	osPackageName  = "os"
)

const mainTerminalOnlyMessage = "main must only delegate to the internal command and handle its terminal error"

const startupSelectorName = "Run"

const terminalHelperSelectorName = "WriteProcessError"

func scanGoRules(root string, paths []string) ([]Violation, error) {
	var violations []Violation
	for _, path := range goFiles(paths) {
		data, err := readRegularFile(root, path)
		if err != nil {
			return nil, err
		}
		if formatted, formatErr := format.Source(data); formatErr == nil && string(formatted) != string(data) {
			violations = append(violations, Violation{
				Rule: "gofmt", Path: path, Line: 1, Column: 1,
				Message: "Go source differs from gofmt output", Fixable: true,
			})
		}
		set := token.NewFileSet()
		file, err := parser.ParseFile(set, path, data, 0)
		if err != nil {
			violations = append(violations, Violation{Rule: "go-parse", Path: path, Line: 1, Column: 1, Message: err.Error()})
			continue
		}
		violations = append(violations, entrypointViolations(set, file, path)...)
		violations = append(violations, prosePinGoViolations(set, file, path, data)...)
		violations = append(violations, instructionHashGoViolations(set, file, path, data)...)
		violations = append(violations, shadowProductionViolations(set, file, path)...)
		violations = append(violations, scenarioSelfTestViolations(set, file, path, data)...)
		violations = append(violations, testSizeViolations(set, file, path)...)
		violations = append(violations, thinWrapperViolations(set, file, path, data)...)
	}
	return violations, nil
}

func entrypointViolations(set *token.FileSet, file *ast.File, path string) []Violation {
	if filepath.Base(path) != "main.go" || file.Name.Name != mainFunctionName || !isCommandMain(path) {
		return nil
	}
	var violations []Violation
	var mainDecl *ast.FuncDecl
	for _, declaration := range file.Decls {
		switch typed := declaration.(type) {
		case *ast.FuncDecl:
			if typed.Name.Name == mainFunctionName {
				mainDecl = typed
				continue
			}
			position := set.Position(typed.Pos())
			violations = append(violations, Violation{
				Rule: "entrypoint-thin", Path: path, Line: position.Line, Column: position.Column,
				Message: "cmd main.go must not contain helper or business functions",
			})
		case *ast.GenDecl:
			if typed.Tok == token.IMPORT {
				continue
			}
			position := set.Position(typed.Pos())
			violations = append(violations, Violation{
				Rule: "entrypoint-thin", Path: path, Line: position.Line, Column: position.Column,
				Message: "cmd main.go must not contain type, const, or var declarations",
			})
		}
	}
	if mainDecl == nil {
		violations = append(violations, Violation{Rule: "entrypoint-thin", Path: path, Line: 1, Column: 1, Message: "cmd main.go must contain main"})
		return violations
	}
	if statementCount(mainDecl.Body) > 8 {
		position := set.Position(mainDecl.Pos())
		violations = append(violations, Violation{
			Rule: "entrypoint-thin", Path: path, Line: position.Line, Column: position.Column,
			Message: "main must only delegate to an internal command and handle its terminal error",
		})
	}
	violations = append(violations, mainBodyStructureViolations(set, path, file, mainDecl)...)
	return violations
}

func mainBodyStructureViolations(set *token.FileSet, path string, file *ast.File, mainDecl *ast.FuncDecl) []Violation {
	if mainDecl.Body == nil {
		return nil
	}
	aliases, _ := processStreamImportAliases(set, file, path)
	checker := &mainBodyChecker{
		set:         set,
		path:        path,
		delegated:   delegatedCommandPackages(mainDecl.Body, aliases),
		errorIdents: make(map[string]bool),
	}
	return checker.violations(mainDecl)
}

func (c *mainBodyChecker) violations(mainDecl *ast.FuncDecl) []Violation {
	var violations []Violation
	if len(c.delegated) != 1 {
		position := c.set.Position(mainDecl.Pos())
		violations = append(violations, Violation{
			Rule: "entrypoint-thin", Path: c.path, Line: position.Line, Column: position.Column,
			Message: "main must delegate startup to exactly one internal command package",
		})
	}
	for _, statement := range mainDecl.Body.List {
		violations = append(violations, c.statementViolations(statement, mainPosition{})...)
	}
	if c.startupCalls != 1 {
		position := c.set.Position(mainDecl.Pos())
		violations = append(violations, Violation{
			Rule: "entrypoint-thin", Path: c.path, Line: position.Line, Column: position.Column,
			Message: "main must contain exactly one startup call into the delegated internal command package",
		})
	}
	return violations
}

func (c *mainBodyChecker) statementViolations(statement ast.Stmt, position mainPosition) []Violation {
	switch typed := statement.(type) {
	case *ast.ExprStmt:
		call, ok := typed.X.(*ast.CallExpr)
		if !ok {
			return []Violation{c.violation(typed.X, mainTerminalOnlyMessage)}
		}
		return c.callViolations(call, position)
	case *ast.AssignStmt:
		return c.assignmentViolations(typed, position)
	case *ast.IfStmt:
		if position.terminal {
			return []Violation{c.violation(typed, "terminal error handling must not branch further")}
		}
		return c.ifViolations(typed)
	case *ast.ReturnStmt:
		if !position.terminal || len(typed.Results) != 0 {
			return []Violation{c.violation(typed, mainTerminalOnlyMessage)}
		}
		return nil
	default:
		return []Violation{c.violation(statement, "main must not contain command dispatch, loops, or feature branching")}
	}
}

func (c *mainBodyChecker) assignmentViolations(assignment *ast.AssignStmt, position mainPosition) []Violation {
	var violations []Violation
	for _, target := range assignment.Lhs {
		if _, ok := target.(*ast.Ident); !ok {
			violations = append(violations, c.violation(target, mainTerminalOnlyMessage))
		}
	}
	startupAssigned := false
	for _, value := range assignment.Rhs {
		call, ok := value.(*ast.CallExpr)
		if !ok {
			violations = append(violations, c.violation(value, mainTerminalOnlyMessage))
			continue
		}
		if !position.terminal && classifyMainCall(call, c.delegated) == mainCallStartup {
			startupAssigned = true
		}
		violations = append(violations, c.callViolations(call, position)...)
	}
	if startupAssigned {
		c.recordStartupErrorIdent(assignment.Lhs)
	}
	return violations
}

func (c *mainBodyChecker) recordStartupErrorIdent(targets []ast.Expr) {
	if len(targets) != 1 {
		return
	}
	identifier, ok := targets[0].(*ast.Ident)
	if ok {
		c.errorIdents[identifier.Name] = true
	}
}

func (c *mainBodyChecker) ifViolations(branch *ast.IfStmt) []Violation {
	var violations []Violation
	if branch.Init != nil {
		violations = append(violations, c.statementViolations(branch.Init, mainPosition{})...)
	}
	errorIdent, bound := errorCheckIdent(branch.Cond)
	terminal := mainPosition{}
	if bound {
		if !c.errorIdents[errorIdent] {
			violations = append(violations, c.violation(branch.Cond, "main must only branch on the startup call's error result"))
		}
		terminal = mainPosition{terminal: true, errorIdent: errorIdent}
	} else {
		violations = append(violations, c.violation(branch.Cond, "main must not branch on CLI arguments or unrelated values"))
	}
	for _, statement := range branch.Body.List {
		violations = append(violations, c.statementViolations(statement, terminal)...)
	}
	return append(violations, c.elseViolations(branch.Else)...)
}

func (c *mainBodyChecker) elseViolations(elseBranch ast.Stmt) []Violation {
	switch typed := elseBranch.(type) {
	case nil:
		return nil
	case *ast.IfStmt:
		return c.ifViolations(typed)
	case *ast.BlockStmt:
		var violations []Violation
		for _, statement := range typed.List {
			violations = append(violations, c.statementViolations(statement, mainPosition{})...)
		}
		return violations
	default:
		return []Violation{c.violation(elseBranch, mainTerminalOnlyMessage)}
	}
}

func (c *mainBodyChecker) callViolations(call *ast.CallExpr, position mainPosition) []Violation {
	switch classifyMainCall(call, c.delegated) {
	case mainCallExit:
		if position.terminal {
			return c.callArgumentViolations(call.Args, position)
		}
		return c.startupExitViolations(call, position)
	case mainCallStderrPrint:
		if !position.terminal {
			return []Violation{c.violation(call, "stderr reporting is only allowed inside terminal handling of the startup call's error")}
		}
		return nil
	case mainCallStartup:
		return append(c.delegatedCallViolations(call, position), c.callArgumentViolations(call.Args, position)...)
	case mainCallTerminalHelper:
		if !position.terminal {
			return []Violation{c.violation(call, "main must delegate startup through the internal command's Run entrypoint")}
		}
		return append(c.delegatedCallViolations(call, position), c.callArgumentViolations(call.Args, position)...)
	default:
		return []Violation{c.violation(call, "main must only call the delegated internal command and terminal error handling")}
	}
}

func (c *mainBodyChecker) startupExitViolations(call *ast.CallExpr, position mainPosition) []Violation {
	wrapsStartup := false
	for _, argument := range call.Args {
		ast.Inspect(argument, func(node ast.Node) bool {
			nested, ok := node.(*ast.CallExpr)
			if ok && classifyMainCall(nested, c.delegated) == mainCallStartup {
				wrapsStartup = true
			}
			return true
		})
	}
	if !wrapsStartup {
		return []Violation{c.violation(call, "os.Exit outside terminal handling must wrap the startup Run call")}
	}
	return c.callArgumentViolations(call.Args, position)
}

func (c *mainBodyChecker) delegatedCallViolations(call *ast.CallExpr, position mainPosition) []Violation {
	if position.terminal {
		if !referencesIdent(call.Args, position.errorIdent) {
			return []Violation{c.violation(call, "terminal delegated calls must handle the startup call's error result")}
		}
		return nil
	}
	c.startupCalls++
	return nil
}

func (c *mainBodyChecker) callArgumentViolations(expressions []ast.Expr, position mainPosition) []Violation {
	var violations []Violation
	for _, expression := range expressions {
		ast.Inspect(expression, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			violations = append(violations, c.callViolations(call, position)...)
			return true
		})
	}
	return violations
}

func (c *mainBodyChecker) violation(node ast.Node, message string) Violation {
	position := c.set.Position(node.Pos())
	return Violation{
		Rule: "entrypoint-thin", Path: c.path, Line: position.Line, Column: position.Column,
		Message: message,
	}
}

func classifyMainCall(call *ast.CallExpr, delegated map[string]bool) mainCallKind {
	packageName, functionName, ok := selectorCallNames(call)
	if !ok {
		return mainCallRejected
	}
	if packageName == osPackageName && functionName == "Exit" {
		return mainCallExit
	}
	if packageName == fmtPackageName && isStderrPrint(functionName) && firstArgumentIsStderr(call) {
		return mainCallStderrPrint
	}
	if delegated[packageName] {
		switch functionName {
		case startupSelectorName:
			return mainCallStartup
		case terminalHelperSelectorName:
			return mainCallTerminalHelper
		default:
			return mainCallRejected
		}
	}
	return mainCallRejected
}

func selectorCallNames(call *ast.CallExpr) (string, string, bool) {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", "", false
	}
	packageIdent, ok := selector.X.(*ast.Ident)
	if !ok {
		return "", "", false
	}
	return packageIdent.Name, selector.Sel.Name, true
}

func isTerminalAuxCall(call *ast.CallExpr, packageName, functionName string) bool {
	if packageName == osPackageName && functionName == "Exit" {
		return true
	}
	return packageName == fmtPackageName && isStderrPrint(functionName) && firstArgumentIsStderr(call)
}

func isStderrPrint(functionName string) bool {
	return functionName == "Fprint" || functionName == "Fprintf" || functionName == "Fprintln"
}

func firstArgumentIsStderr(call *ast.CallExpr) bool {
	if len(call.Args) == 0 {
		return false
	}
	selector, ok := call.Args[0].(*ast.SelectorExpr)
	return ok && isPackageSelector(selector, osPackageName, "Stderr")
}

func isPackageSelector(selector *ast.SelectorExpr, packageName, name string) bool {
	packageIdent, ok := selector.X.(*ast.Ident)
	return ok && packageIdent.Name == packageName && selector.Sel.Name == name
}

func delegatedCommandPackages(body *ast.BlockStmt, aliases map[string]string) map[string]bool {
	packages := make(map[string]bool)
	ast.Inspect(body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		packageName, functionName, ok := selectorCallNames(call)
		if !ok || isTerminalAuxCall(call, packageName, functionName) {
			return true
		}
		if isInternalCommandImport(aliases[packageName]) {
			packages[packageName] = true
		}
		return true
	})
	return packages
}

func isInternalCommandImport(importPath string) bool {
	return strings.Contains(importPath, "/internal/")
}

func errorCheckIdent(condition ast.Expr) (string, bool) {
	comparison, ok := condition.(*ast.BinaryExpr)
	if !ok || comparison.Op != token.NEQ {
		return "", false
	}
	if name, ok := nilCheckOperand(comparison.X); ok && isNilIdent(comparison.Y) {
		return name, true
	}
	if name, ok := nilCheckOperand(comparison.Y); ok && isNilIdent(comparison.X) {
		return name, true
	}
	return "", false
}

func nilCheckOperand(expression ast.Expr) (string, bool) {
	identifier, ok := expression.(*ast.Ident)
	if !ok || identifier.Name == "nil" {
		return "", false
	}
	return identifier.Name, true
}

func isNilIdent(expression ast.Expr) bool {
	identifier, ok := expression.(*ast.Ident)
	return ok && identifier.Name == "nil"
}

func referencesIdent(expressions []ast.Expr, name string) bool {
	found := false
	for _, expression := range expressions {
		ast.Inspect(expression, func(node ast.Node) bool {
			identifier, ok := node.(*ast.Ident)
			if ok && identifier.Name == name {
				found = true
			}
			return !found
		})
	}
	return found
}

func isCommandMain(path string) bool {
	normalized := "/" + filepath.ToSlash(path)
	return strings.Contains(normalized, "/cmd/")
}

func prosePinGoViolations(set *token.FileSet, file *ast.File, path string, data []byte) []Violation {
	if !strings.HasSuffix(path, "_test.go") {
		return nil
	}
	var violations []Violation
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Body == nil || !isGoTestEntrypoint(function.Name.Name) {
			continue
		}
		segment := nodeText(data, set.Position(function.Pos()).Offset, set.Position(function.End()).Offset)
		if hasDocumentReference(segment) {
			violations = append(violations, prosePinFunctionViolations(set, path, function)...)
		}
	}
	return violations
}

func prosePinFunctionViolations(set *token.FileSet, path string, function *ast.FuncDecl) []Violation {
	var violations []Violation
	ast.Inspect(function.Body, func(node ast.Node) bool {
		violations = append(violations, prosePinNodeViolations(set, path, node)...)
		return true
	})
	return violations
}

func prosePinNodeViolations(set *token.FileSet, path string, node ast.Node) []Violation {
	switch typed := node.(type) {
	case *ast.CallExpr:
		if !isStringPinCall(typed.Fun) {
			return nil
		}
		return proseLiteralViolations(set, path, typed.Args, "test must not pin long natural-language instruction or Markdown prose")
	case *ast.BinaryExpr:
		if typed.Op != token.EQL && typed.Op != token.NEQ {
			return nil
		}
		return proseLiteralViolations(set, path, []ast.Expr{typed.X, typed.Y}, "test must not exact-pin long natural-language instruction or Markdown prose")
	default:
		return nil
	}
}

func proseLiteralViolations(set *token.FileSet, path string, expressions []ast.Expr, message string) []Violation {
	var violations []Violation
	for _, expression := range expressions {
		value, ok := stringLiteral(expression)
		if !ok || !proseLike(value) {
			continue
		}
		position := set.Position(expression.Pos())
		violations = append(violations, Violation{
			Rule: "prose-contract-pin", Path: path, Line: position.Line, Column: position.Column,
			Message: message,
		})
	}
	return violations
}

func instructionHashGoViolations(set *token.FileSet, file *ast.File, path string, data []byte) []Violation {
	if !strings.HasSuffix(path, "_test.go") || !importsPackage(file, "crypto/sha256") {
		return nil
	}
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Body == nil || !isGoTestEntrypoint(function.Name.Name) {
			continue
		}
		segment := nodeText(data, set.Position(function.Pos()).Offset, set.Position(function.End()).Offset)
		if !hasDocumentReference(segment) || !containsSHA256Reference(segment) {
			continue
		}
		position := set.Position(function.Pos())
		return []Violation{{
			Rule: "instruction-content-hash", Path: path, Line: position.Line, Column: position.Column,
			Message: "tests must not make whole instruction or Markdown file hashes a contract",
		}}
	}
	return nil
}

func importsPackage(file *ast.File, packagePath string) bool {
	for _, spec := range file.Imports {
		value, err := strconv.Unquote(spec.Path.Value)
		if err == nil && value == packagePath {
			return true
		}
	}
	return false
}

func containsSHA256Reference(segment string) bool {
	return strings.Contains(segment, "sha256") || strings.Contains(segment, "SHA256")
}

func shadowProductionViolations(set *token.FileSet, file *ast.File, path string) []Violation {
	if !strings.HasSuffix(path, "_test.go") {
		return nil
	}
	prefixes := []string{"orchestrate", "simulate", "applyUpdate", "updateAndVerify", "verificationOutcome", "fetchWakeReset", "wakeFailure"}
	var violations []Violation
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Body == nil || isGoTestEntrypoint(function.Name.Name) {
			continue
		}
		matched := false
		for _, prefix := range prefixes {
			if strings.HasPrefix(function.Name.Name, prefix) {
				matched = true
				break
			}
		}
		if !matched || branchCount(function.Body) < 3 {
			continue
		}
		position := set.Position(function.Pos())
		violations = append(violations, Violation{
			Rule: "test-shadow-production", Path: path, Line: position.Line, Column: position.Column,
			Message: "test helper reimplements orchestration or state-machine behavior instead of driving production",
		})
	}
	return violations
}

func scenarioSelfTestViolations(set *token.FileSet, file *ast.File, path string, data []byte) []Violation {
	if !strings.HasSuffix(path, "_test.go") || strings.Contains(path, "/internal/harnesslint/") || !strings.Contains(string(data), "scenarios") {
		return nil
	}
	var violations []Violation
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || !strings.HasPrefix(function.Name.Name, "Test") || function.Body == nil {
			continue
		}
		segment := nodeText(data, set.Position(function.Pos()).Offset, set.Position(function.End()).Offset)
		lower := strings.ToLower(segment)
		if !strings.Contains(function.Name.Name, "CorpusContract") &&
			!strings.Contains(lower, "scenariocount") &&
			!strings.Contains(lower, "requiredscenario") &&
			!strings.Contains(lower, "manifest.") {
			continue
		}
		position := set.Position(function.Pos())
		violations = append(violations, Violation{
			Rule: "scenario-self-test", Path: path, Line: position.Line, Column: position.Column,
			Message: "scenario corpus must drive production behavior, not maintain a second self-test contract",
		})
	}
	return violations
}

func testSizeViolations(set *token.FileSet, file *ast.File, path string) []Violation {
	if !strings.HasSuffix(path, "_test.go") {
		return nil
	}
	var violations []Violation
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Body == nil || !isGoTestEntrypoint(function.Name.Name) {
			continue
		}
		lines := set.Position(function.End()).Line - set.Position(function.Pos()).Line + 1
		statements := statementCount(function.Body)
		if lines <= 150 && statements <= 100 {
			continue
		}
		position := set.Position(function.Pos())
		violations = append(violations, Violation{
			Rule: "test-size-limit", Path: path, Line: position.Line, Column: position.Column,
			Message: "test entrypoint exceeds 150 lines or 100 statements; split by behavior instead of hiding assertions in helpers",
		})
	}
	return violations
}

func thinWrapperViolations(set *token.FileSet, file *ast.File, path string, data []byte) []Violation {
	if strings.HasSuffix(path, "_test.go") || hasBuildConstraint(data) {
		return nil
	}
	var violations []Violation
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if violation, ok := thinWrapperViolation(set, path, function); ok {
			violations = append(violations, violation)
		}
	}
	return violations
}

func thinWrapperViolation(set *token.FileSet, path string, function *ast.FuncDecl) (Violation, bool) {
	if function.Recv != nil || function.Body == nil || function.Name.IsExported() || function.Name.Name == mainFunctionName {
		return Violation{}, false
	}
	parameters := parameterNames(function.Type.Params)
	if len(parameters) == 0 || len(function.Body.List) != 1 {
		return Violation{}, false
	}
	call := forwardedCall(function.Body.List[0])
	if call == nil || !argumentsForward(parameters, call.Args) {
		return Violation{}, false
	}
	position := set.Position(function.Pos())
	return Violation{
		Rule: "thin-wrapper-proliferation", Path: path, Line: position.Line, Column: position.Column,
		Message: "private forwarding wrapper adds no validation, transformation, or ownership boundary",
	}, true
}

func hasBuildConstraint(data []byte) bool {
	prefix := string(data)
	if len(prefix) > 1024 {
		prefix = prefix[:1024]
	}
	return strings.Contains(prefix, "//go:build") || strings.Contains(prefix, "// +build")
}

func statementCount(node ast.Node) int {
	count := 0
	ast.Inspect(node, func(current ast.Node) bool {
		if _, ok := current.(ast.Stmt); ok {
			count++
		}
		return true
	})
	return count
}

func branchCount(node ast.Node) int {
	count := 0
	ast.Inspect(node, func(current ast.Node) bool {
		switch current.(type) {
		case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt, *ast.SwitchStmt, *ast.TypeSwitchStmt, *ast.SelectStmt:
			count++
		case *ast.CaseClause:
			count++
		}
		return true
	})
	return count
}

func isGoTestEntrypoint(name string) bool {
	return strings.HasPrefix(name, "Test") || strings.HasPrefix(name, "Benchmark") ||
		strings.HasPrefix(name, "Fuzz") || strings.HasPrefix(name, "Example")
}

func parameterNames(fields *ast.FieldList) []string {
	if fields == nil {
		return nil
	}
	var names []string
	for _, field := range fields.List {
		if len(field.Names) == 0 {
			return nil
		}
		for _, name := range field.Names {
			names = append(names, name.Name)
		}
	}
	return names
}

func forwardedCall(statement ast.Stmt) *ast.CallExpr {
	switch typed := statement.(type) {
	case *ast.ReturnStmt:
		if len(typed.Results) != 1 {
			return nil
		}
		call, _ := typed.Results[0].(*ast.CallExpr)
		return call
	case *ast.ExprStmt:
		call, _ := typed.X.(*ast.CallExpr)
		return call
	default:
		return nil
	}
}

func argumentsForward(parameters []string, arguments []ast.Expr) bool {
	if len(parameters) != len(arguments) {
		return false
	}
	for index, argument := range arguments {
		identifier, ok := argument.(*ast.Ident)
		if !ok || identifier.Name != parameters[index] {
			return false
		}
	}
	return true
}

func isStringPinCall(expression ast.Expr) bool {
	selector, ok := expression.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := selector.X.(*ast.Ident)
	if !ok || (pkg.Name != "strings" && pkg.Name != "bytes") {
		return false
	}
	switch selector.Sel.Name {
	case "Contains", "EqualFold", "HasPrefix", "HasSuffix":
		return true
	default:
		return false
	}
}

func stringLiteral(expression ast.Expr) (string, bool) {
	literal, ok := expression.(*ast.BasicLit)
	if !ok || literal.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(literal.Value)
	return value, err == nil
}

func hasDocumentReference(text string) bool {
	return strings.Contains(text, ".md") || strings.Contains(text, "codex/instructions/") ||
		strings.Contains(text, "codex/glm-worker/prompts/") || strings.Contains(text, "AGENTS")
}

func proseLike(value string) bool {
	if utf8.RuneCountInString(value) < 32 {
		return false
	}
	words := len(strings.Fields(value))
	hasJapanese := false
	for _, value := range value {
		if unicode.In(value, unicode.Hiragana, unicode.Katakana, unicode.Han) {
			hasJapanese = true
			break
		}
	}
	return hasJapanese || words >= 5
}

func nodeText(data []byte, start, end int) string {
	if start < 0 || end < start || start > len(data) {
		return ""
	}
	if end > len(data) {
		end = len(data)
	}
	return string(data[start:end])
}
