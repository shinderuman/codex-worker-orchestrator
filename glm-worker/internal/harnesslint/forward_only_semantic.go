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

const (
	forwardOnlyOldTransport uint8 = 1 << iota
	forwardOnlyCurrentTransport
	forwardOnlyTransportWalkLimit = 4
)

type forwardOnlySemanticFunction struct {
	path             string
	set              *token.FileSet
	decl             *ast.FuncDecl
	calls            map[string]struct{}
	directTransports uint8
	directWait       bool
	oldTypeWrite     bool
	waitNameWrite    bool
}

type forwardOnlySemanticPackage struct {
	functions map[string]*forwardOnlySemanticFunction
}

func scanForwardOnlySemanticCompatibility(root string, paths []string) ([]Violation, error) {
	packages, err := parseForwardOnlySemanticPackages(root, paths)
	if err != nil {
		return nil, err
	}
	var violations []Violation
	for _, pkg := range packages {
		violations = append(violations, forwardOnlySemanticPackageViolations(pkg)...)
	}
	return violations, nil
}

func parseForwardOnlySemanticPackages(root string, paths []string) (map[string]*forwardOnlySemanticPackage, error) {
	packages := make(map[string]*forwardOnlySemanticPackage)
	for _, filePath := range paths {
		if !strings.HasSuffix(filePath, ".go") || forwardOnlyFixturePath(filePath) {
			continue
		}
		data, err := readRegularFile(root, filePath)
		if err != nil {
			return nil, err
		}
		set := token.NewFileSet()
		file, err := parser.ParseFile(set, filePath, data, 0)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", filePath, err)
		}
		key := path.Dir(filePath) + "\x00" + file.Name.Name
		pkg := packages[key]
		if pkg == nil {
			pkg = &forwardOnlySemanticPackage{functions: make(map[string]*forwardOnlySemanticFunction)}
			packages[key] = pkg
		}
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil || function.Recv != nil {
				continue
			}
			pkg.functions[function.Name.Name] = forwardOnlySemanticFunctionFacts(filePath, set, function)
		}
	}
	return packages, nil
}

func forwardOnlySemanticFunctionFacts(filePath string, set *token.FileSet, function *ast.FuncDecl) *forwardOnlySemanticFunction {
	facts := &forwardOnlySemanticFunction{
		path:  filePath,
		set:   set,
		decl:  function,
		calls: make(map[string]struct{}),
	}
	ast.Inspect(function.Body, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.Ident:
			facts.directTransports |= forwardOnlyTransportIdentifier(typed.Name)
			facts.directWait = facts.directWait || forwardOnlyWaitIdentifier(typed.Name)
		case *ast.BasicLit:
			if typed.Kind != token.STRING {
				break
			}
			value, err := strconv.Unquote(typed.Value)
			if err != nil {
				break
			}
			facts.directTransports |= forwardOnlyTransportLiteral(value)
			facts.directWait = facts.directWait || forwardOnlyWaitLiteral(value)
		case *ast.CallExpr:
			if identifier, ok := forwardOnlyUnparen(typed.Fun).(*ast.Ident); ok {
				facts.calls[identifier.Name] = struct{}{}
			}
		case *ast.AssignStmt:
			forwardOnlySemanticAssignmentFacts(facts, typed)
		}
		return true
	})
	return facts
}

func forwardOnlySemanticAssignmentFacts(facts *forwardOnlySemanticFunction, assignment *ast.AssignStmt) {
	for index, target := range assignment.Lhs {
		selector, ok := forwardOnlyUnparen(target).(*ast.SelectorExpr)
		if !ok || index >= len(assignment.Rhs) {
			continue
		}
		switch selector.Sel.Name {
		case "Type":
			if forwardOnlyExpressionTransport(assignment.Rhs[index])&forwardOnlyOldTransport != 0 {
				facts.oldTypeWrite = true
			}
		case "Name":
			if forwardOnlyExpressionWait(assignment.Rhs[index]) {
				facts.waitNameWrite = true
			}
		}
	}
}

func forwardOnlySemanticPackageViolations(pkg *forwardOnlySemanticPackage) []Violation {
	var violations []Violation
	for _, function := range pkg.functions {
		if strings.HasSuffix(function.path, "_test.go") {
			if function.directTransports == forwardOnlyOldTransport|forwardOnlyCurrentTransport && function.directWait {
				violations = append(violations, forwardOnlyViolation(function.set, function.path, function.decl.Name,
					"tests must not preserve mixed old/current parent-wait transport acceptance"))
			}
			continue
		}
		if function.directTransports == forwardOnlyOldTransport|forwardOnlyCurrentTransport && function.directWait {
			violations = append(violations, forwardOnlyViolation(function.set, function.path, function.decl.Name,
				"parent-wait consumers must not accept old and current transports in parallel"))
			continue
		}
		if function.directTransports&forwardOnlyCurrentTransport != 0 && forwardOnlyFunctionRewritesCurrentWait(pkg, function, forwardOnlyTransportWalkLimit, nil) {
			violations = append(violations, forwardOnlyViolation(function.set, function.path, function.decl.Name,
				"current parent-wait transport must not be normalized or rewritten into the old transport"))
		}
	}
	return violations
}

func forwardOnlyFunctionRewritesCurrentWait(pkg *forwardOnlySemanticPackage, function *forwardOnlySemanticFunction, remaining int, seen map[string]bool) bool {
	if function.oldTypeWrite && function.waitNameWrite {
		return true
	}
	if remaining == 0 {
		return false
	}
	if seen == nil {
		seen = make(map[string]bool)
	}
	name := function.decl.Name.Name
	if seen[name] {
		return false
	}
	seen[name] = true
	defer delete(seen, name)
	for called := range function.calls {
		next := pkg.functions[called]
		if next != nil && forwardOnlyFunctionRewritesCurrentWait(pkg, next, remaining-1, seen) {
			return true
		}
	}
	return false
}

func forwardOnlyTransportIdentifier(name string) uint8 {
	switch name {
	case "codexRolloutFunctionCallType", "codexRolloutFunctionCallOutputType":
		return forwardOnlyOldTransport
	case "codexRolloutCustomToolCallType", "codexRolloutCustomToolCallOutputType":
		return forwardOnlyCurrentTransport
	default:
		return 0
	}
}

func forwardOnlyTransportLiteral(value string) uint8 {
	switch value {
	case "function_call", "function_call_output":
		return forwardOnlyOldTransport
	case "custom_tool_call", "custom_tool_call_output":
		return forwardOnlyCurrentTransport
	default:
		return 0
	}
}

func forwardOnlyWaitIdentifier(name string) bool {
	return name == "codexRolloutWaitCallName" || name == "analysisWaitYieldMSKey"
}

func forwardOnlyWaitLiteral(value string) bool {
	return value == "wait" || value == "tools.write_stdin" || value == "yield-time_ms" || value == "yield-time-ms"
}

func forwardOnlyExpressionTransport(expression ast.Expr) uint8 {
	switch typed := forwardOnlyUnparen(expression).(type) {
	case *ast.Ident:
		return forwardOnlyTransportIdentifier(typed.Name)
	case *ast.BasicLit:
		if typed.Kind != token.STRING {
			return 0
		}
		value, err := strconv.Unquote(typed.Value)
		if err != nil {
			return 0
		}
		return forwardOnlyTransportLiteral(value)
	default:
		return 0
	}
}

func forwardOnlyExpressionWait(expression ast.Expr) bool {
	switch typed := forwardOnlyUnparen(expression).(type) {
	case *ast.Ident:
		return forwardOnlyWaitIdentifier(typed.Name)
	case *ast.BasicLit:
		if typed.Kind != token.STRING {
			return false
		}
		value, err := strconv.Unquote(typed.Value)
		return err == nil && forwardOnlyWaitLiteral(value)
	default:
		return false
	}
}
