package workflow

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

type parentBehaviorEvalRegistry struct {
	Version     int                      `json:"version"`
	Description string                   `json:"description"`
	Cases       []parentBehaviorEvalCase `json:"cases"`
}

type parentBehaviorEvalCase struct {
	ID              string   `json:"id"`
	ContractSources []string `json:"contract_sources"`
	Positive        string   `json:"positive"`
	Negative        string   `json:"negative"`
	Evidence        string   `json:"evidence"`
	RunPolicy       string   `json:"run_policy"`
	Status          string   `json:"status"`
}

func loadParentBehaviorEvals(t *testing.T) map[string]parentBehaviorEvalCase {
	t.Helper()
	root := scenarioRepoRoot(t)
	path := filepath.Join(root, "tests", "parent-behavior-evals.json")
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()

	dec := json.NewDecoder(f)
	dec.DisallowUnknownFields()
	var registry parentBehaviorEvalRegistry
	if err := dec.Decode(&registry); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		t.Fatalf("%s must contain exactly one JSON value: %v", path, err)
	}
	if registry.Version != 1 {
		t.Fatalf("parent behavior eval registry version = %d want 1", registry.Version)
	}
	if strings.TrimSpace(registry.Description) == "" {
		t.Fatal("parent behavior eval registry description must not be empty")
	}

	result := make(map[string]parentBehaviorEvalCase, len(registry.Cases))
	for _, c := range registry.Cases {
		if c.ID == "" || len(c.ContractSources) == 0 || strings.TrimSpace(c.Positive) == "" || strings.TrimSpace(c.Negative) == "" || strings.TrimSpace(c.Evidence) == "" {
			t.Errorf("parent behavior eval has incomplete fields: %+v", c)
			continue
		}
		if c.RunPolicy != "explicit-user-authorization" || c.Status != "not-run" {
			t.Errorf("parent behavior eval %s has unexpected execution state: run_policy=%q status=%q", c.ID, c.RunPolicy, c.Status)
		}
		if _, exists := result[c.ID]; exists {
			t.Errorf("duplicate parent behavior eval id %q", c.ID)
			continue
		}
		for _, source := range c.ContractSources {
			info, err := os.Lstat(filepath.Join(root, filepath.FromSlash(source)))
			if err != nil {
				t.Errorf("parent behavior eval %s contract source %s: %v", c.ID, source, err)
				continue
			}
			if !info.Mode().IsRegular() {
				t.Errorf("parent behavior eval %s contract source %s is not a regular file", c.ID, source)
			}
		}
		result[c.ID] = c
	}
	return result
}

func requireParentBehaviorEval(t *testing.T, id string) {
	t.Helper()
	if _, ok := loadParentBehaviorEvals(t)[id]; !ok {
		t.Fatalf("parent behavior eval registry lacks %q", id)
	}
}

func TestParentBehaviorEvalRegistryIsMinimalAuthority(t *testing.T) {
	root := scenarioRepoRoot(t)
	if _, err := os.Lstat(filepath.Join(root, "EVAL.md")); !os.IsNotExist(err) {
		if err == nil {
			t.Fatal("EVAL.md must stay absent; deterministic evals belong to tests/scenarios and live parent cases to tests/parent-behavior-evals.json")
		}
		t.Fatalf("stat EVAL.md: %v", err)
	}

	got := loadParentBehaviorEvals(t)
	want := []string{
		"commit-authorization",
		"escaped-cause-layer",
		"failure-evidence",
		"feasibility-gate",
		"high-semantic-postcondition",
		"plan-commit-sync",
		"task-lifecycle",
	}
	ids := make([]string, 0, len(got))
	for id := range got {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	if strings.Join(ids, "\n") != strings.Join(want, "\n") {
		t.Fatalf("parent behavior eval ids = %v want %v", ids, want)
	}
}
