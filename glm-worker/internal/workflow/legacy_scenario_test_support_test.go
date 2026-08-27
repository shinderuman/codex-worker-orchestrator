package workflow

import (
	"os"
	"path/filepath"
	"testing"
)

type scenarioArtifact struct {
	Name    string
	Content string
}

type scenarioDoc struct {
	ID                        string
	ExpectedTelemetryClock    string
	WorkerMutatesPlanFile     bool
	ReviewerMutatesWorktree   bool
	ReportOnlyMutatesWorktree bool
	PlanFileInitiallyAbsent   bool
	PlanFileTrackedAbsent     bool
}

type scenarioFile struct {
	Scenarios []scenarioDoc
}

type manifestEntry struct {
	Path      string
	Scenarios []string
}

type manifestFile struct {
	InstructionFiles []manifestEntry
}

const (
	scenarioArtifactDirToken    = "{{ARTIFACT_DIR}}"
	telemetryClockInjectedStart = "injected-start"
)

func scenarioRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "glm-worker", "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("repository root not found from %s", dir)
		}
		dir = parent
	}
}

func loadCorpus(t *testing.T) (scenarioFile, manifestFile) {
	t.Helper()
	t.Skip("legacy scenario corpus was removed; remaining caller must be converted to direct behavior coverage")
	return scenarioFile{}, manifestFile{}
}
