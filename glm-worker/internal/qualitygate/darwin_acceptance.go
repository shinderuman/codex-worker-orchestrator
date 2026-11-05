package qualitygate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/build/constraint"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// DarwinPackageRequirement is part of the source-bound canonical Go gate record.
// A package PASS alone is insufficient when Darwin-tagged tests are present.
type DarwinPackageRequirement struct {
	Package string   `json:"package"`
	Tests   []string `json:"tests,omitempty"`
}

// ChangedDarwinPackages finds Darwin-only source in the candidate commit and
// any uncommitted source. Unrelated Tasks retain the existing supplement scope.
func ChangedDarwinPackages(repository string) ([]DarwinPackageRequirement, error) {
	changed := map[string]bool{}
	for _, args := range [][]string{
		{"diff-tree", "--root", "-r", "--no-commit-id", "--name-only", "-z", "HEAD"},
		{"diff", "--name-only", "-z", "HEAD"},
		{"ls-files", "--others", "--exclude-standard", "-z"},
	} {
		command := exec.Command("git", append([]string{"-C", repository}, args...)...)
		out, err := command.Output()
		if err != nil {
			return nil, fmt.Errorf("inspect Darwin candidate source: %w", err)
		}
		for _, name := range bytes.Split(out, []byte{0}) {
			if len(name) == 0 {
				continue
			}
			path := filepath.ToSlash(string(name))
			if !strings.HasPrefix(path, "glm-worker/") || !strings.HasSuffix(path, ".go") {
				continue
			}
			if darwinSourceFile(filepath.Join(repository, filepath.FromSlash(path)), path) {
				changed[filepath.ToSlash(filepath.Dir(strings.TrimPrefix(path, "glm-worker/")))] = true
			}
		}
	}
	dirs := make([]string, 0, len(changed))
	for dir := range changed {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	result := make([]DarwinPackageRequirement, 0, len(dirs))
	for _, dir := range dirs {
		moduleDir := filepath.Join(repository, "glm-worker", filepath.FromSlash(dir))
		entries, err := os.ReadDir(moduleDir)
		if os.IsNotExist(err) {
			continue // A fully removed package no longer needs compilation.
		}
		if err != nil {
			return nil, err
		}
		hasGo := false
		tests := map[string]bool{}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
				continue
			}
			hasGo = true
			name := entry.Name()
			if !strings.HasSuffix(name, "_test.go") || !darwinSourceFile(filepath.Join(moduleDir, name), name) {
				continue
			}
			parsed, err := parser.ParseFile(token.NewFileSet(), filepath.Join(moduleDir, name), nil, 0)
			if err != nil {
				return nil, fmt.Errorf("parse Darwin acceptance tests %s: %w", name, err)
			}
			for _, decl := range parsed.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if ok && fn.Recv == nil && goTestFunctionName(fn.Name.Name) {
					tests[fn.Name.Name] = true
				}
			}
		}
		if !hasGo {
			continue
		}
		requirement := DarwinPackageRequirement{Package: "./" + dir}
		for name := range tests {
			requirement.Tests = append(requirement.Tests, name)
		}
		sort.Strings(requirement.Tests)
		result = append(result, requirement)
	}
	return result, nil
}

// TestMain is a special runner hook, not a test with a pass event.
func goTestFunctionName(name string) bool {
	if name == "TestMain" || !strings.HasPrefix(name, "Test") || len(name) == 4 {
		return false
	}
	r := []rune(strings.TrimPrefix(name, "Test"))[0]
	return r < 'a' || r > 'z'
}

func darwinSourceFile(path, name string) bool {
	if strings.HasSuffix(name, "_darwin.go") || strings.HasSuffix(name, "_darwin_test.go") {
		return true
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return darwinSourceBytes(name, data)
}

// ChangedDarwinPackagesAtCommit recomputes the required scope from immutable
// Git objects, independently of the saved run record and released checkout.
// Publication must not accept a run that omitted a candidate's Darwin tests.
func ChangedDarwinPackagesAtCommit(repository, commit string) ([]DarwinPackageRequirement, error) {
	if len(commit) != 40 {
		return nil, fmt.Errorf("invalid candidate commit identity")
	}
	for _, ch := range commit {
		if ch < '0' || (ch > '9' && ch < 'a') || ch > 'f' {
			return nil, fmt.Errorf("invalid candidate commit identity")
		}
	}
	changed, err := darwinGitOutput(repository, "diff-tree", "--root", "-r", "--no-commit-id", "--name-only", "-z", commit)
	if err != nil {
		return nil, err
	}
	dirs := map[string]bool{}
	for _, entry := range bytes.Split(changed, []byte{0}) {
		path := string(entry)
		if !strings.HasPrefix(path, "glm-worker/") || !strings.HasSuffix(path, ".go") {
			continue
		}
		target, showErr := darwinGitOutput(repository, "show", commit+":"+path)
		if showErr != nil {
			// A removed file may still have carried a Darwin build constraint.
			target, showErr = darwinGitOutput(repository, "show", commit+"^:"+path)
		}
		if showErr == nil && darwinSourceBytes(filepath.Base(path), target) {
			dirs[filepath.ToSlash(filepath.Dir(strings.TrimPrefix(path, "glm-worker/")))] = true
		}
	}
	keys := make([]string, 0, len(dirs))
	for dir := range dirs {
		keys = append(keys, dir)
	}
	sort.Strings(keys)
	required := make([]DarwinPackageRequirement, 0, len(keys))
	for _, dir := range keys {
		entries, err := darwinGitOutput(repository, "ls-tree", "-r", "--name-only", "-z", commit, "--", "glm-worker/"+dir)
		if err != nil {
			return nil, err
		}
		hasGo := false
		tests := map[string]bool{}
		for _, entry := range bytes.Split(entries, []byte{0}) {
			path := string(entry)
			if filepath.ToSlash(filepath.Dir(strings.TrimPrefix(path, "glm-worker/"))) != dir || !strings.HasSuffix(path, ".go") {
				continue
			}
			hasGo = true
			if !strings.HasSuffix(path, "_test.go") {
				continue
			}
			body, err := darwinGitOutput(repository, "show", commit+":"+path)
			if err != nil {
				return nil, err
			}
			if !darwinSourceBytes(filepath.Base(path), body) {
				continue
			}
			parsed, err := parser.ParseFile(token.NewFileSet(), path, body, 0)
			if err != nil {
				return nil, fmt.Errorf("parse candidate Darwin tests %s: %w", path, err)
			}
			for _, decl := range parsed.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if ok && fn.Recv == nil && strings.HasPrefix(fn.Name.Name, "Test") {
					tests[fn.Name.Name] = true
				}
			}
		}
		if !hasGo {
			continue
		}
		item := DarwinPackageRequirement{Package: "./" + dir}
		for name := range tests {
			item.Tests = append(item.Tests, name)
		}
		sort.Strings(item.Tests)
		required = append(required, item)
	}
	return required, nil
}

func darwinGitOutput(repository string, args ...string) ([]byte, error) {
	out, err := exec.Command("git", append([]string{"-C", repository}, args...)...).Output()
	if err != nil {
		return nil, fmt.Errorf("inspect source-bound Darwin acceptance: %w", err)
	}
	return out, nil
}

func darwinSourceBytes(name string, data []byte) bool {
	if strings.HasSuffix(name, "_darwin.go") || strings.HasSuffix(name, "_darwin_test.go") {
		return true
	}
	for _, line := range strings.SplitN(string(data), "\n", 30) {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "//go:build ") {
			continue
		}
		expr, err := constraint.Parse(line)
		if err != nil {
			return false
		}
		darwin := expr.Eval(func(tag string) bool { return tag == "darwin" || tag == "arm64" })
		linux := expr.Eval(func(tag string) bool { return tag == "linux" || tag == "arm64" })
		return darwin && !linux
	}
	return false
}

func DarwinRequirementPackages(requirements []DarwinPackageRequirement) []string {
	packages := make([]string, 0, len(requirements))
	for _, requirement := range requirements {
		packages = append(packages, requirement.Package)
	}
	return packages
}

// VerifyDarwinAcceptance rejects skipped/missing tests and packages, including
// a successfully exited go test that did not actually execute a required test.
func VerifyDarwinAcceptance(log []byte, requirements []DarwinPackageRequirement) error {
	if len(requirements) == 0 {
		return nil
	}
	type event struct {
		Action  string
		Package string
		Test    string
	}
	packages := map[string]bool{}
	tests := map[string]map[string]bool{}
	for _, line := range bytes.Split(log, []byte("\n")) {
		var item event
		if json.Unmarshal(line, &item) != nil || item.Action != "pass" {
			continue
		}
		for _, requirement := range requirements {
			suffix := "/" + strings.TrimPrefix(requirement.Package, "./")
			if !strings.HasSuffix(item.Package, suffix) {
				continue
			}
			if item.Test == "" {
				packages[requirement.Package] = true
			} else {
				if tests[requirement.Package] == nil {
					tests[requirement.Package] = map[string]bool{}
				}
				tests[requirement.Package][item.Test] = true
			}
		}
	}
	for _, requirement := range requirements {
		if !packages[requirement.Package] {
			return fmt.Errorf("required Darwin package did not PASS: %s", requirement.Package)
		}
		for _, name := range requirement.Tests {
			if !tests[requirement.Package][name] {
				return fmt.Errorf("required Darwin test did not PASS: %s/%s", requirement.Package, name)
			}
		}
	}
	return nil
}
