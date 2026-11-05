package controller

import (
	"bytes"
	"fmt"
	"go/build"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskcontract"
)

func (s *Store) requireChangedDarwinPackageCoverage(admission Admission, tree string) error {
	paths, err := runGitBinary(s.identity.PrimaryRoot, nil, "diff", "--name-only", "-z", "--no-renames", "--diff-filter=AM", admission.Head.IntegrationTip, tree, "--", "glm-worker/*.go")
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		return nil
	}
	tests, err := s.taskDarwinAcceptance(admission)
	if err != nil {
		return err
	}
	covered := make(map[string]bool)
	for _, test := range tests {
		covered[test.Package] = true
	}
	for _, file := range strings.Split(string(paths), "\x00") {
		relative, ok := strings.CutPrefix(file, "glm-worker/")
		if !ok {
			continue
		}
		dir := path.Dir(relative)
		pkg := "."
		if dir != "." {
			pkg = "./" + dir
		}
		if covered[pkg] {
			continue
		}
		body, err := runGitBinary(s.identity.PrimaryRoot, nil, "cat-file", "blob", tree+":"+file)
		if err != nil {
			return err
		}
		if err := requireDarwinPackageDeclaration(file, pkg, body); err != nil {
			return err
		}
	}
	return nil
}

func requireDarwinPackageDeclaration(file, pkg string, body []byte) error {
	ctx := build.Default
	ctx.GOARCH = "arm64"
	ctx.OpenFile = func(string) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(body)), nil }
	for _, cgo := range []bool{false, true} {
		ctx.CgoEnabled = cgo
		only, err := isDarwinOnlyGoFile(ctx, path.Base(file))
		if err != nil {
			return err
		}
		if only {
			return fmt.Errorf("changed Darwin-only source %s requires a Task Darwin acceptance test for %s", file, pkg)
		}
	}
	return nil
}

func isDarwinOnlyGoFile(ctx build.Context, filename string) (bool, error) {
	ctx.GOOS = "darwin"
	darwin, err := ctx.MatchFile("", filename)
	if err != nil || !darwin {
		return false, err
	}
	ctx.GOOS = "linux"
	linux, err := ctx.MatchFile("", filename)
	return !linux, err
}

func (s *Store) applyTaskDarwinAcceptance(admission Admission, forms []FinalValidationRequest) error {
	tests, err := s.taskDarwinAcceptance(admission)
	if err != nil || len(tests) == 0 {
		return err
	}
	for i := range forms {
		if forms[i].Form == "go-test" {
			forms[i].DarwinTests, err = qualitygate.NormalizeDarwinTests(append(forms[i].DarwinTests, tests...))
			return err
		}
	}
	return fmt.Errorf("declared Darwin acceptance requires go-test")
}

func (s *Store) taskDarwinAcceptance(admission Admission) ([]qualitygate.DarwinTest, error) {
	task := admission.Attempt.SemanticTaskRef
	body, err := s.darwinAcceptanceTaskContent(admission, task)
	if err != nil {
		return nil, err
	}
	if digestBytes(body) != task.ContractDigest {
		return nil, fmt.Errorf("task authority changed before resolving Darwin acceptance")
	}
	entries, err := taskcontract.ParseDarwinAcceptance(body)
	if err != nil {
		return nil, err
	}
	tests := make([]qualitygate.DarwinTest, 0, len(entries))
	for _, entry := range entries {
		pkg, name, _ := strings.Cut(entry, ":")
		tests = append(tests, qualitygate.DarwinTest{Package: pkg, Name: name})
	}
	return tests, nil
}

func (s *Store) darwinAcceptanceTaskContent(admission Admission, task SemanticTaskRef) ([]byte, error) {
	if task.RequestID != "" {
		request, err := s.requestTaskAuthority(task)
		if err != nil {
			return nil, err
		}
		return []byte(request.Content), nil
	}
	body, err := os.ReadFile(filepath.Join(admission.Workspace.Root, task.TaskPath))
	if err != nil {
		return nil, fmt.Errorf("read Task Darwin acceptance: %w", err)
	}
	return body, nil
}
