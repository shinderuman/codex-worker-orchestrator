package harnesslint

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/parser"
	"go/token"
	"go/types"
	"strconv"
	"strings"
)

type qualityWiringCheck struct {
	path            string
	tokens          []string
	forbiddenTokens []string
	orderedTokens   []string
}

type qualityWiringGoEvidence struct {
	tokens       map[string]bool
	bindingError error
	file         *ast.File
	bindings     types.Info
	importName   string
}

func scanQualitySurface(root string, paths []string) ([]Violation, error) {
	wiring, err := qualityWiringViolations(root, paths)
	if err != nil {
		return nil, err
	}
	streams, err := processStreamViolations(root, paths)
	if err != nil {
		return nil, err
	}
	return append(wiring, streams...), nil
}

func qualityWiringViolations(root string, paths []string) ([]Violation, error) {
	present := make(map[string]bool, len(paths))
	for _, path := range paths {
		present[path] = true
	}
	var violations []Violation
	workflowViolations, err := qualityWiringPackageViolations(root, present, "glm-worker/internal/workflow/", []string{
		"w.captureQualitySurfaceBaseline()",
		"w.verifyQualitySurfaceBaseline(workerPhase)",
		"w.qualityGate(w.config.RepoRoot)",
		"harnesslint.IsViolation(qualityReport)",
	})
	if err != nil {
		return nil, err
	}
	violations = append(violations, workflowViolations...)
	for _, check := range qualityWiringChecks() {
		current, err := qualityWiringCheckViolations(root, present, check)
		if err != nil {
			return nil, err
		}
		violations = append(violations, current...)
	}
	return violations, nil
}

func qualityWiringChecks() []qualityWiringCheck {
	checks := []qualityWiringCheck{
		{
			path: "glm-worker/internal/workflow/quality_gate.go",
			tokens: []string{
				"harnesslint.Run(root, true)",
				"captureQualitySurfaceDigest",
			},
			forbiddenTokens: []string{
				"harnesslint.Check(root)",
			},
		},
		{
			path: "install.sh",
			tokens: []string{
				"quality-tools.yml",
				"QUALITY_TOOLS_BIN_DIR",
				"quality_tool_path",
				"require_quality_tool",
				"DEADCODE_VERSION=$(quality_contract_value deadcode)",
				"require_quality_tool deadcode \"$DEADCODE_VERSION\"",
				"./cmd/harnesslint",
				"./cmd/plancheck",
				"./cmd/repo-cli-install",
				"\"$build_dir/repo-cli-install\" -mode install -build-dir \"$build_dir\" -bin-dir \"$bin_dir\"",
				"result=$(\"$build_dir/merge-json\" -fragment \"$repo_root/claude/settings-managed.json\")",
				"\"$build_dir/plancheck\" \"$repo_root\"",
			},
		},
		{
			path: ".githooks/post-merge",
			tokens: []string{
				"exec \"$repo_root/install.sh\"",
			},
		},
		{
			path: "harnesslint",
			tokens: []string{
				"quality-tools.yml",
				"GOTOOLCHAIN",
				"GOCACHE",
				"run ./cmd/harnesslint",
			},
		},
	}
	return append(checks, qualityToolWiringChecks()...)
}

func qualityToolWiringChecks() []qualityWiringCheck {
	return []qualityWiringCheck{
		{
			path: "quality-tools.yml",
			tokens: []string{
				"namespace:",
				"default-bin-dir:",
				"go:",
				"lint-go:",
				"golangci-lint:",
				"deadcode:",
				"shellcheck:",
				"shfmt:",
			},
		},
		ciWorkflowWiringCheck(),
		installSmokeWorkflowWiringCheck(),
		{
			path: "install-quality-tools.sh",
			tokens: []string{
				"quality-tools.yml",
				"QUALITY_TOOLS_BIN_DIR",
				"tool_namespace=$(contract_value namespace)",
				"default_bin_dir=$(contract_value default-bin-dir)",
				"deadcode_version=$(contract_value deadcode)",
				"quality_tool_path",
				"target_needs_install",
				"golang.org/x/tools/cmd/deadcode@v$deadcode_version",
				"golangci-lint/releases/download",
				"shellcheck/releases/download",
				"go install",
			},
		},
		{
			path: "tests/install_quality_tools_smoke.sh",
			tokens: []string{
				"user-owned-$tool",
				"codex-worker-orchestrator-deadcode-0.49.0",
				"codex-worker-orchestrator-shfmt-3.13.1",
				"codex-worker-orchestrator-shfmt-3.13.2",
				"quality tool collision:",
			},
		},
	}
}

func ciWorkflowWiringCheck() qualityWiringCheck {
	return qualityWiringCheck{
		path: ".github/workflows/ci.yml",
		tokens: []string{
			"quality-tools.yml",
			"quality-tools.outputs.go_version",
			"QUALITY_TOOLS_BIN_DIR",
			"$GITHUB_ENV",
			"./install-quality-tools.sh",
			"./tests/install_quality_tools_smoke.sh",
			"./.github/workflows/install-smoke.yml",
		},
	}
}

func installSmokeWorkflowWiringCheck() qualityWiringCheck {
	return qualityWiringCheck{
		path: ".github/workflows/install-smoke.yml",
		tokens: []string{
			"workflow_call:",
			"quality-tools.yml",
			"quality-tools.outputs.go_version",
			"QUALITY_TOOLS_BIN_DIR",
			"$GITHUB_ENV",
			"./install-quality-tools.sh",
			"./tests/install_smoke.sh",
			"./tests/install_detached_runtime_identity_smoke.sh",
		},
	}
}

func qualityWiringPackageViolations(root string, present map[string]bool, prefix string, tokens []string) ([]Violation, error) {
	required, err := qualityWiringRequiredCalls(tokens)
	if err != nil {
		return nil, err
	}
	found := make(map[string]bool, len(required))
	packageFound := false
	for path := range present {
		if !strings.HasPrefix(path, prefix) || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			continue
		}
		packageFound = true
		if err := scanQualityWiringCalls(root, path, required, found); err != nil {
			return nil, err
		}
	}
	path := strings.TrimSuffix(prefix, "/")
	if !packageFound {
		return []Violation{{Rule: "quality-wiring", Path: path, Line: 1, Column: 1, Message: "required quality-gate package is missing"}}, nil
	}
	var violations []Violation
	for _, token := range tokens {
		if found[token] {
			continue
		}
		violations = append(violations, Violation{
			Rule: "quality-wiring", Path: path, Line: 1, Column: 1,
			Message: "required quality-gate wiring is missing: " + token,
		})
	}
	return violations, nil
}

func qualityWiringRequiredCalls(tokens []string) (map[string]string, error) {
	required := make(map[string]string, len(tokens))
	for _, candidate := range tokens {
		expr, err := parser.ParseExpr(candidate)
		if err != nil {
			return nil, err
		}
		call, ok := expr.(*ast.CallExpr)
		if !ok {
			return nil, fmt.Errorf("quality-wiring token is not a call: %q", candidate)
		}
		key, ok := qualityWiringSelectorCallKey(call, "w", harnesslintName)
		if !ok {
			return nil, fmt.Errorf("quality-wiring token is not a selector call: %q", candidate)
		}
		required[candidate] = key
	}
	return required, nil
}

func scanQualityWiringCalls(root, path string, required map[string]string, found map[string]bool) error {
	data, err := readRegularFile(root, path)
	if err != nil {
		return err
	}
	file, err := parser.ParseFile(token.NewFileSet(), path, data, 0)
	if err != nil {
		return fmt.Errorf("parse quality wiring %s: %w", path, err)
	}
	importName := qualityWiringHarnesslintImport(file)
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Body == nil {
			continue
		}
		scanQualityWiringFunction(function, required, found, importName)
	}
	return nil
}

func scanQualityWiringFunction(function *ast.FuncDecl, required map[string]string, found map[string]bool, importName string) {
	receiver := qualityWiringWorkflowReceiver(function)
	ast.Inspect(function.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		key, ok := qualityWiringSelectorCallKey(call, receiver, importName)
		if !ok {
			return true
		}
		for candidate, requiredKey := range required {
			if key == requiredKey {
				found[candidate] = true
			}
		}
		return true
	})
}

func qualityWiringWorkflowReceiver(function *ast.FuncDecl) string {
	if function.Recv == nil || len(function.Recv.List) != 1 {
		return ""
	}
	decl := function.Recv.List[0]
	if len(decl.Names) != 1 {
		return ""
	}
	receiverType := decl.Type
	if pointer, ok := receiverType.(*ast.StarExpr); ok {
		receiverType = pointer.X
	}
	ident, ok := receiverType.(*ast.Ident)
	if !ok || ident.Name != "Workflow" {
		return ""
	}
	return decl.Names[0].Name
}

func qualityWiringSelectorCallKey(call *ast.CallExpr, workflowReceiver, importName string) (string, bool) {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	base, ok := selector.X.(*ast.Ident)
	if !ok {
		return "", false
	}
	var prefix string
	switch {
	case workflowReceiver != "" && base.Name == workflowReceiver:
		prefix = "workflow"
	case importName != "" && base.Name == importName:
		prefix = harnesslintName
	default:
		return "", false
	}
	args := make([]string, 0, len(call.Args))
	for _, arg := range call.Args {
		identity, ok := qualityWiringArgumentKey(arg, workflowReceiver)
		if !ok {
			return "", false
		}
		args = append(args, identity)
	}
	return prefix + "." + selector.Sel.Name + "(" + strings.Join(args, ",") + ")", true
}

func qualityWiringArgumentKey(expression ast.Expr, workflowReceiver string) (string, bool) {
	switch node := expression.(type) {
	case *ast.Ident:
		if workflowReceiver != "" && node.Name == workflowReceiver {
			return "workflow", true
		}
		return node.Name, true
	case *ast.SelectorExpr:
		base, ok := qualityWiringArgumentKey(node.X, workflowReceiver)
		if !ok {
			return "", false
		}
		return base + "." + node.Sel.Name, true
	case *ast.ParenExpr:
		return qualityWiringArgumentKey(node.X, workflowReceiver)
	default:
		return "", false
	}
}

func qualityWiringCheckViolations(root string, present map[string]bool, check qualityWiringCheck) ([]Violation, error) {
	if !present[check.path] {
		return []Violation{{
			Rule: "quality-wiring", Path: check.path, Line: 1, Column: 1,
			Message: "required quality-gate file is missing",
		}}, nil
	}
	data, err := readRegularFile(root, check.path)
	if err != nil {
		return nil, err
	}
	text := string(data)
	requiredGo, err := qualityWiringGoTokensForPath(check.path, data)
	if err != nil {
		return nil, err
	}
	var violations []Violation
	for _, token := range check.tokens {
		if qualityWiringTokenPresent(text, requiredGo, token) {
			continue
		}
		violations = append(violations, Violation{
			Rule: "quality-wiring", Path: check.path, Line: 1, Column: 1,
			Message: qualityWiringMissingTokenMessage(token, requiredGo),
		})
	}
	forbidden, err := qualityWiringForbiddenCalls(check, data, requiredGo)
	if err != nil {
		return nil, err
	}
	for _, call := range forbidden {
		violations = append(violations, Violation{
			Rule: "quality-wiring", Path: check.path, Line: 1, Column: 1,
			Message: "forbidden quality-gate wiring is present: " + call,
		})
	}
	orderViolations, err := qualityWiringOrderViolations(check, data)
	if err != nil {
		return nil, err
	}
	violations = append(violations, orderViolations...)
	return violations, nil
}

func qualityWiringGoTokensForPath(path string, data []byte) (qualityWiringGoEvidence, error) {
	if !strings.HasSuffix(path, ".go") {
		return qualityWiringGoEvidence{}, nil
	}
	return qualityWiringRequiredGoTokens(path, data)
}

func qualityWiringTokenPresent(text string, detected qualityWiringGoEvidence, token string) bool {
	if result, ok := detected.tokens[token]; ok {
		return result
	}
	return strings.Contains(text, token)
}

func qualityWiringRequiredGoTokens(path string, data []byte) (qualityWiringGoEvidence, error) {
	set := token.NewFileSet()
	file, err := parser.ParseFile(set, path, data, 0)
	if err != nil {
		return qualityWiringGoEvidence{}, err
	}
	bindings, bindingError := qualityWiringPartialBindings(set, file)
	found := map[string]bool{
		"harnesslint.Run(root, true)": false,
		"captureQualitySurfaceDigest": false,
	}
	harnessImport := qualityWiringHarnesslintImport(file)
	ast.Inspect(file, func(node ast.Node) bool {
		if qualityWiringHasDigestEvidence(node) {
			found["captureQualitySurfaceDigest"] = true
		}
		if function, ok := node.(*ast.FuncDecl); ok && function.Recv == nil &&
			function.Name.Name == "runRepositoryQualityGate" && function.Body != nil {
			found["harnesslint.Run(root, true)"] = qualityWiringBodyCallsHarnessRun(function, harnessImport, &bindings)
		}
		return true
	})
	return qualityWiringGoEvidence{tokens: found, bindingError: bindingError, file: file, bindings: bindings, importName: harnessImport}, nil
}

func qualityWiringHasDigestEvidence(node ast.Node) bool {
	switch current := node.(type) {
	case *ast.FuncDecl:
		return current.Recv == nil && current.Name.Name == "captureQualitySurfaceDigest"
	case *ast.CallExpr:
		identifier, ok := current.Fun.(*ast.Ident)
		return ok && identifier.Name == "captureQualitySurfaceDigest"
	default:
		return false
	}
}

func qualityWiringMissingTokenMessage(token string, evidence qualityWiringGoEvidence) string {
	message := "required quality-gate wiring is missing: " + token
	if evidence.bindingError != nil {
		message += "; binding context: " + evidence.bindingError.Error()
	}
	return message
}

func qualityWiringPartialBindings(set *token.FileSet, file *ast.File) (types.Info, error) {
	bindings := types.Info{Defs: make(map[*ast.Ident]types.Object), Uses: make(map[*ast.Ident]types.Object)}
	configuration := types.Config{Importer: qualityWiringNameImporter{}, Error: func(error) {}}
	_, err := configuration.Check(file.Name.Name, set, []*ast.File{file}, &bindings)
	return bindings, err
}

type qualityWiringNameImporter struct{}

func (qualityWiringNameImporter) Import(importPath string) (*types.Package, error) {
	name := importPath[strings.LastIndexByte(importPath, '/')+1:]
	pkg := types.NewPackage(importPath, name)
	pkg.MarkComplete()
	return pkg, nil
}

func qualityWiringBodyCallsHarnessRun(function *ast.FuncDecl, harnessImport string, bindings *types.Info) bool {
	parameters := function.Type.Params
	if parameters == nil || len(parameters.List) != 1 || len(parameters.List[0].Names) != 1 {
		return false
	}
	root := bindings.Defs[parameters.List[0].Names[0]]
	if root == nil {
		return false
	}
	found := false
	ast.Inspect(function.Body, func(node ast.Node) bool {
		if call, ok := node.(*ast.CallExpr); ok && qualityWiringIsHarnessRun(call, harnessImport, root, bindings) {
			found = true
		}
		return true
	})
	return found
}

func qualityWiringIsHarnessRun(call *ast.CallExpr, harnessImport string, root types.Object, bindings *types.Info) bool {
	if len(call.Args) != 2 || !qualityWiringIsImportedCall(call, harnessImport, "Run", bindings) {
		return false
	}
	argument, ok := ast.Unparen(call.Args[0]).(*ast.Ident)
	if !ok || bindings.Uses[argument] != root {
		return false
	}
	fix, ok := ast.Unparen(call.Args[1]).(*ast.Ident)
	if !ok {
		return false
	}
	value, ok := bindings.Uses[fix].(*types.Const)
	return ok && value.Val().Kind() == constant.Bool && constant.BoolVal(value.Val())
}

func qualityWiringIsImportedCall(call *ast.CallExpr, importName, member string, bindings *types.Info) bool {
	selector, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != member {
		return false
	}
	pkg, ok := selector.X.(*ast.Ident)
	if !ok || importName == "" || pkg.Name != importName {
		return false
	}
	_, bound := bindings.Uses[pkg].(*types.PkgName)
	return bound
}

func qualityWiringForbiddenCalls(check qualityWiringCheck, data []byte, evidence qualityWiringGoEvidence) ([]string, error) {
	if len(check.forbiddenTokens) == 0 {
		return nil, nil
	}
	if !strings.HasSuffix(check.path, ".go") {
		return qualityWiringForbiddenText(check.forbiddenTokens, data), nil
	}
	if evidence.file == nil {
		return nil, fmt.Errorf("quality wiring Go evidence is unavailable: %s", check.path)
	}
	return qualityWiringForbiddenGo(evidence), nil
}

func qualityWiringForbiddenText(values []string, data []byte) []string {
	var found []string
	text := string(data)
	for _, value := range values {
		if strings.Contains(text, value) {
			found = append(found, value)
		}
	}
	return found
}

func qualityWiringForbiddenGo(evidence qualityWiringGoEvidence) []string {
	found := false
	ast.Inspect(evidence.file, func(node ast.Node) bool {
		if qualityWiringIsHarnessCheck(node, evidence.importName, &evidence.bindings) {
			found = true
		}
		return true
	})
	if found {
		return []string{"harnesslint.Check"}
	}
	return nil
}

func qualityWiringIsHarnessCheck(node ast.Node, importName string, bindings *types.Info) bool {
	call, ok := node.(*ast.CallExpr)
	return ok && qualityWiringIsImportedCall(call, importName, "Check", bindings)
}

func qualityWiringHarnesslintImport(file *ast.File) string {
	for _, imported := range file.Imports {
		path, err := strconv.Unquote(imported.Path.Value)
		if err != nil || path != "github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/harnesslint" {
			continue
		}
		if imported.Name == nil {
			return harnesslintName
		}
		if imported.Name.Name != "_" && imported.Name.Name != "." {
			return imported.Name.Name
		}
	}
	return ""
}

func qualityWiringOrderViolations(check qualityWiringCheck, data []byte) ([]Violation, error) {
	if len(check.orderedTokens) == 0 {
		return nil, nil
	}
	positions, err := qualityWiringOrderPositions(check, data)
	if err != nil {
		return nil, err
	}
	if qualityWiringOrderValid(check.orderedTokens, positions) {
		return nil, nil
	}
	return []Violation{{
		Rule: "quality-wiring", Path: check.path, Line: 1, Column: 1,
		Message: "required quality-gate wiring order is invalid: " + strings.Join(check.orderedTokens, " -> "),
	}}, nil
}

func qualityWiringOrderPositions(check qualityWiringCheck, data []byte) (map[string]int, error) {
	if strings.HasSuffix(check.path, ".go") {
		return qualityWiringGoCallPositions(check, data)
	}
	return qualityWiringTextPositions(check.orderedTokens, data), nil
}

func qualityWiringGoCallPositions(check qualityWiringCheck, data []byte) (map[string]int, error) {
	set := token.NewFileSet()
	file, err := parser.ParseFile(set, check.path, data, 0)
	if err != nil {
		return nil, err
	}
	wanted := make(map[string]bool, len(check.orderedTokens))
	for _, required := range check.orderedTokens {
		wanted[required] = true
	}
	positions := make(map[string]int, len(check.orderedTokens))
	ast.Inspect(file, func(node ast.Node) bool {
		recordQualityWiringCallPosition(set, data, wanted, positions, node)
		return true
	})
	return positions, nil
}

func recordQualityWiringCallPosition(set *token.FileSet, data []byte, wanted map[string]bool, positions map[string]int, node ast.Node) {
	call, ok := node.(*ast.CallExpr)
	if !ok {
		return
	}
	start := set.Position(call.Pos()).Offset
	end := set.Position(call.End()).Offset
	if start < 0 || end > len(data) || start >= end {
		return
	}
	text := string(data[start:end])
	if !wanted[text] {
		return
	}
	if _, exists := positions[text]; !exists {
		positions[text] = start
	}
}

func qualityWiringTextPositions(ordered []string, data []byte) map[string]int {
	positions := make(map[string]int, len(ordered))
	text := string(data)
	for _, required := range ordered {
		if index := strings.Index(text, required); index >= 0 {
			positions[required] = index
		}
	}
	return positions
}

func qualityWiringOrderValid(ordered []string, positions map[string]int) bool {
	lastIndex := -1
	for _, required := range ordered {
		index, ok := positions[required]
		if !ok || index <= lastIndex {
			return false
		}
		lastIndex = index
	}
	return true
}
