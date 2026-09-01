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

const scenarioArtifactDirToken = "{{ARTIFACT_DIR}}"

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
