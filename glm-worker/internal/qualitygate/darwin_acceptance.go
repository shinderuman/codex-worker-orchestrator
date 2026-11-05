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
				if ok && fn.Recv == nil && strings.HasPrefix(fn.Name.Name, "Test") {
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

func darwinSourceFile(path, name string) bool {
	// The Go filename suffix itself limits the file to Darwin.
	if strings.HasSuffix(name, "_darwin.go") || strings.HasSuffix(name, "_darwin_test.go") {
		return true
	}
	data, err := os.ReadFile(path)
	if err != nil {
		// Deleted or renamed files are included when their name is Darwin-specific.
		return false
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
