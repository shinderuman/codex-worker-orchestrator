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

type DarwinPackageRequirement struct {
	Package string   `json:"package"`
	Tests   []string `json:"tests,omitempty"`
}

func ChangedDarwinPackages(repository string) ([]DarwinPackageRequirement, error) {
	changed, err := darwinWorkingTreeChanges(repository)
	if err != nil {
		return nil, err
	}
	dirs, err := darwinChangedDirs(changed, func(path string) (bool, error) {
		return darwinChangedPath(repository, path), nil
	})
	if err != nil {
		return nil, err
	}
	return darwinRequirements(dirs, func(dir string) (DarwinPackageRequirement, bool, error) {
		return darwinWorktreeRequirement(repository, dir)
	})
}

func darwinWorkingTreeChanges(repository string) ([]string, error) {
	var changed []string
	for _, args := range [][]string{
		{"diff-tree", "--root", "-r", "--no-commit-id", "--name-only", "-z", "HEAD"},
		{"diff", "--name-only", "-z", "HEAD"},
		{"ls-files", "--others", "--exclude-standard", "-z"},
	} {
		out, err := darwinGitOutput(repository, args...)
		if err != nil {
			return nil, err
		}
		changed = append(changed, darwinLines(out)...)
	}
	return changed, nil
}

func darwinLines(data []byte) []string {
	var paths []string
	for _, entry := range bytes.Split(data, []byte{0}) {
		if len(entry) != 0 {
			paths = append(paths, string(entry))
		}
	}
	return paths
}

func darwinChangedDirs(paths []string, eligible func(string) (bool, error)) ([]string, error) {
	found := map[string]bool{}
	for _, path := range paths {
		path = filepath.ToSlash(path)
		if !strings.HasPrefix(path, "glm-worker/") || !strings.HasSuffix(path, ".go") {
			continue
		}
		required, err := eligible(path)
		if err != nil {
			return nil, err
		}
		if required {
			found[filepath.ToSlash(filepath.Dir(strings.TrimPrefix(path, "glm-worker/")))] = true
		}
	}
	dirs := make([]string, 0, len(found))
	for dir := range found {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	return dirs, nil
}

func darwinRequirements(dirs []string, load func(string) (DarwinPackageRequirement, bool, error)) ([]DarwinPackageRequirement, error) {
	var required []DarwinPackageRequirement
	for _, dir := range dirs {
		item, exists, err := load(dir)
		if err != nil {
			return nil, err
		}
		if exists {
			required = append(required, item)
		}
	}
	return required, nil
}

func darwinWorktreeRequirement(repository, dir string) (DarwinPackageRequirement, bool, error) {
	moduleDir := filepath.Join(repository, "glm-worker", filepath.FromSlash(dir))
	entries, err := os.ReadDir(moduleDir)
	if os.IsNotExist(err) {
		return DarwinPackageRequirement{}, false, nil
	}
	if err != nil {
		return DarwinPackageRequirement{}, false, err
	}
	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".go") {
			files = append(files, entry.Name())
		}
	}
	return darwinPackageFromFiles(dir, files, func(name string) ([]byte, error) {
		return os.ReadFile(filepath.Join(moduleDir, name))
	})
}

func darwinPackageFromFiles(dir string, files []string, read func(string) ([]byte, error)) (DarwinPackageRequirement, bool, error) {
	if len(files) == 0 {
		return DarwinPackageRequirement{}, false, nil
	}
	tests := map[string]bool{}
	for _, name := range files {
		if !strings.HasSuffix(name, "_test.go") {
			continue
		}
		body, err := read(name)
		if err != nil {
			return DarwinPackageRequirement{}, false, err
		}
		if !darwinSourceBytes(filepath.Base(name), body) {
			continue
		}
		names, err := darwinTestNames(name, body)
		if err != nil {
			return DarwinPackageRequirement{}, false, err
		}
		for _, test := range names {
			tests[test] = true
		}
	}
	required := DarwinPackageRequirement{Package: "./" + dir}
	for name := range tests {
		required.Tests = append(required.Tests, name)
	}
	sort.Strings(required.Tests)
	return required, true, nil
}

func darwinTestNames(name string, body []byte) ([]string, error) {
	parsed, err := parser.ParseFile(token.NewFileSet(), name, body, 0)
	if err != nil {
		return nil, fmt.Errorf("parse Darwin acceptance tests %s: %w", name, err)
	}
	var names []string
	for _, decl := range parsed.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Recv == nil && goTestFunctionName(fn.Name.Name) {
			names = append(names, fn.Name.Name)
		}
	}
	return names, nil
}

func goTestFunctionName(name string) bool {
	if name == "TestMain" || !strings.HasPrefix(name, "Test") || len(name) == 4 {
		return false
	}
	r := []rune(strings.TrimPrefix(name, "Test"))[0]
	return r < 'a' || r > 'z'
}

func darwinChangedPath(repository, path string) bool {
	name := filepath.Base(path)
	absolute := filepath.Join(repository, filepath.FromSlash(path))
	if darwinSourceFile(absolute, name) {
		return true
	}
	if _, err := os.Stat(absolute); err == nil {
		return false
	}
	previous, err := darwinGitOutput(repository, "show", "HEAD^:"+path)
	return err == nil && darwinSourceBytes(name, previous)
}

func darwinSourceFile(path, name string) bool {
	if darwinFilename(name) {
		return true
	}
	data, err := os.ReadFile(path)
	return err == nil && darwinSourceBytes(name, data)
}

func ChangedDarwinPackagesAtCommit(repository, commit string) ([]DarwinPackageRequirement, error) {
	if !darwinCommitOID(commit) {
		return nil, fmt.Errorf("invalid candidate commit identity")
	}
	raw, err := darwinGitOutput(repository, "diff-tree", "--root", "-r", "--no-commit-id", "--name-only", "-z", commit)
	if err != nil {
		return nil, err
	}
	dirs, err := darwinChangedDirs(darwinLines(raw), func(path string) (bool, error) {
		return darwinChangedCommitPath(repository, commit, path)
	})
	if err != nil {
		return nil, err
	}
	return darwinRequirements(dirs, func(dir string) (DarwinPackageRequirement, bool, error) {
		return darwinCommitRequirement(repository, commit, dir)
	})
}

func darwinCommitOID(commit string) bool {
	if len(commit) != 40 {
		return false
	}
	for _, ch := range commit {
		if ch < '0' || (ch > '9' && ch < 'a') || ch > 'f' {
			return false
		}
	}
	return true
}

func darwinChangedCommitPath(repository, commit, path string) (bool, error) {
	body, err := darwinGitOutput(repository, "show", commit+":"+path)
	if err != nil {
		body, err = darwinGitOutput(repository, "show", commit+"^:"+path)
	}
	if err != nil {
		return false, err
	}
	return darwinSourceBytes(filepath.Base(path), body), nil
}

func darwinCommitRequirement(repository, commit, dir string) (DarwinPackageRequirement, bool, error) {
	raw, err := darwinGitOutput(repository, "ls-tree", "-r", "--name-only", "-z", commit, "--", "glm-worker/"+dir)
	if err != nil {
		return DarwinPackageRequirement{}, false, err
	}
	var files []string
	for _, path := range darwinLines(raw) {
		name := strings.TrimPrefix(path, "glm-worker/")
		if filepath.ToSlash(filepath.Dir(name)) == dir && strings.HasSuffix(path, ".go") {
			files = append(files, path)
		}
	}
	return darwinPackageFromFiles(dir, files, func(name string) ([]byte, error) {
		return darwinGitOutput(repository, "show", commit+":"+name)
	})
}

func darwinGitOutput(repository string, args ...string) ([]byte, error) {
	out, err := exec.Command("git", append([]string{"-C", repository}, args...)...).Output()
	if err != nil {
		return nil, fmt.Errorf("inspect source-bound Darwin acceptance: %w", err)
	}
	return out, nil
}

func darwinFilename(name string) bool {
	base := strings.TrimSuffix(name, ".go")
	base = strings.TrimSuffix(base, "_test")
	return strings.HasSuffix(base, "_darwin") || strings.Contains(base, "_darwin_")
}

func darwinSourceBytes(name string, data []byte) bool {
	if darwinFilename(name) {
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
		darwinRecordPass(item.Package, item.Test, requirements, packages, tests)
	}
	for _, requirement := range requirements {
		if err := darwinVerifyRequirement(requirement, packages, tests); err != nil {
			return err
		}
	}
	return nil
}

func darwinRecordPass(pkg, test string, requirements []DarwinPackageRequirement, packages map[string]bool, tests map[string]map[string]bool) {
	for _, requirement := range requirements {
		suffix := "/" + strings.TrimPrefix(requirement.Package, "./")
		if !strings.HasSuffix(pkg, suffix) {
			continue
		}
		if test == "" {
			packages[requirement.Package] = true
			continue
		}
		if tests[requirement.Package] == nil {
			tests[requirement.Package] = map[string]bool{}
		}
		tests[requirement.Package][test] = true
	}
}

func darwinVerifyRequirement(required DarwinPackageRequirement, packages map[string]bool, tests map[string]map[string]bool) error {
	if !packages[required.Package] {
		return fmt.Errorf("required Darwin package did not PASS: %s", required.Package)
	}
	for _, name := range required.Tests {
		if !tests[required.Package][name] {
			return fmt.Errorf("required Darwin test did not PASS: %s/%s", required.Package, name)
		}
	}
	return nil
}
