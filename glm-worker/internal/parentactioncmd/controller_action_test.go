package parentactioncmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentaction"
)

func TestControllerParentTransportPreservesPayloadAndRejectsReplay(t *testing.T) {
	for _, action := range []string{"controller-semantic", "controller-execution", "controller-publication", "controller-evidence"} {
		t.Run(action, func(t *testing.T) {
			cfg := newCanonicalCutoverConfig(t, true)
			bin := t.TempDir()
			stub := "#!/bin/sh\n[ \"$1\" = --authority ] || exit 2\n[ \"$2\" = " + action + " ] || exit 3\ncat\n"
			if err := os.WriteFile(filepath.Join(bin, "glm-worker"), []byte(stub), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			before, err := controller.CaptureWorkspaceSnapshot(cfg.RepoRoot)
			if err != nil {
				t.Fatal(err)
			}
			prepared, err := parentaction.Prepare(cfg.RepoRoot, action)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(prepared.Path)
			if err != nil {
				t.Fatal(err)
			}
			payload := `{"action":"recover","transition_id":"exact-identity"}`
			raw = bytes.Replace(raw, []byte("__GLM_PARENT_ACTION_PAYLOAD__"), []byte(payload), 1)
			if err := os.WriteFile(prepared.Path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			after, err := controller.CaptureWorkspaceSnapshot(cfg.RepoRoot)
			if err != nil || before != after {
				t.Fatalf("staging changed admitted snapshot: %v", err)
			}
			if _, err := controller.Activate(cfg); err != nil {
				t.Fatalf("staging revoked live lease: %v", err)
			}
			var out bytes.Buffer
			if err := execute(cfg, []string{action, prepared.Token}, &out, &bytes.Buffer{}); err != nil {
				t.Fatal(err)
			}
			if strings.TrimSpace(out.String()) != payload {
				t.Fatalf("payload changed: %s", out.String())
			}
			if err := execute(cfg, []string{action, prepared.Token}, &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
				t.Fatal("consumed token replayed")
			}
		})
	}
}

func TestControllerParentTransportKeepsStageAfterRejectedCommand(t *testing.T) {
	cfg := newCanonicalCutoverConfig(t, false)
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "glm-worker"), []byte("#!/bin/sh\nexit 7\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	prepared, err := parentaction.Prepare(cfg.RepoRoot, "controller-execution")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(prepared.Path)
	if err != nil {
		t.Fatal(err)
	}
	raw = bytes.Replace(raw, []byte("__GLM_PARENT_ACTION_PAYLOAD__"), []byte(`{"action":"materialize","expected_generation":0}`), 1)
	if err := os.WriteFile(prepared.Path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := execute(cfg, []string{prepared.Action, prepared.Token}, &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
		t.Fatal("rejected controller command succeeded")
	}
	if _, err := parentaction.Peek(cfg.RepoRoot, prepared.Action, prepared.Token); err != nil {
		t.Fatalf("rejected command consumed stage: %v", err)
	}
}
