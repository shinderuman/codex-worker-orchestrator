package workflow

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestQualitySurfaceChangeStopsBeforeReviewer(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{{structured: implementedPacket("initial")}}}
	var out bytes.Buffer
	w := newWorkflowTWithOutput(t, st, r, &out)
	calls := 0
	w.captureQualitySurface = func(string) (string, error) {
		calls++
		if calls == 1 {
			return "baseline", nil
		}
		return "changed", nil
	}

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if len(r.phases) != 1 || r.phases[0] != "worker-new" {
		t.Fatalf("reviewer must not run after quality surface change: %v", r.phases)
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("status = %s", st.TaskStatus())
	}
	if !strings.Contains(out.String(), `"status":"NEEDS_SOL_REVIEW"`) || !strings.Contains(out.String(), "quality policy surface") {
		t.Fatalf("fail-closed packet missing: %s", out.String())
	}
}

func TestCaptureQualitySurfaceDigestTracksPolicyContent(t *testing.T) {
	root := t.TempDir()
	runQualityGateGit(t, root, "init")
	writeQualityGateFile(t, root, "glm-worker/go.mod", "module github.com/shinderuman/codex-worker-orchestrator/glm-worker\n")
	writeQualityGateFile(t, root, ".golangci.yml", "version: one\n")
	runQualityGateGit(t, root, "add", ".")

	before, err := captureQualitySurfaceDigest(root)
	if err != nil {
		t.Fatal(err)
	}
	writeQualityGateFile(t, root, ".golangci.yml", "version: two\n")
	after, err := captureQualitySurfaceDigest(root)
	if err != nil {
		t.Fatal(err)
	}
	if before == after {
		t.Fatal("quality surface digest did not change")
	}
}

func runQualityGateGit(t *testing.T, root string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", root}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}

func writeQualityGateFile(t *testing.T, root, path, content string) {
	t.Helper()
	absolute := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(absolute, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
