package app

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryharness"
)

func TestExecuteFailsClosedBeforeModelCallOnInvalidRepositoryQualityScope(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(t *testing.T, root string)
		reason string
	}{
		{
			name: "module missing",
			mutate: func(t *testing.T, root string) {
				if err := os.Remove(filepath.Join(root, "glm-worker", "go.mod")); err != nil {
					t.Fatal(err)
				}
			},
			reason: repositoryharness.QualityScopeModuleMissing,
		},
		{
			name: "module mismatch",
			mutate: func(t *testing.T, root string) {
				if err := os.WriteFile(filepath.Join(root, "glm-worker", "go.mod"), []byte("module example.com/foreign\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			reason: repositoryharness.QualityScopeModuleMismatch,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := newQualityContractConfig(t)
			test.mutate(t, cfg.RepoRoot)
			r := &fakeRunner{steps: []fakeStep{{structured: implementedPacketApp("done")}}}
			stubQualityPreflight(t, func(string) error {
				t.Fatal("invalid repository quality scope reached quality tool preflight")
				return nil
			})

			err := Execute(Command{Mode: ModeNewTask, Payload: "request"}, cfg, r.factory(), io.Discard, io.Discard)
			var preflightErr *RepositoryHarnessPreflightError
			if err == nil || !errors.As(err, &preflightErr) {
				t.Fatalf("repository harness failure was not preserved: %v", err)
			}
			if preflightErr.Reason != test.reason {
				t.Fatalf("reason = %q want %q", preflightErr.Reason, test.reason)
			}
			if len(r.prompts) != 0 {
				t.Fatalf("model calls = %d", len(r.prompts))
			}
		})
	}
}

func TestWriteProcessErrorRepositoryHarnessFailureHasNoQualityToolRepairGuidance(t *testing.T) {
	cause := &repositoryharness.QualityScopeError{Reason: repositoryharness.QualityScopeModuleMissing}
	err := newRepositoryHarnessPreflightError(cause)
	envelope, raw := writeProcessErrorJSON(t, err)
	if envelope.Error.Kind != errorKindRepositoryHarnessFailed {
		t.Fatalf("kind = %q: %s", envelope.Error.Kind, raw)
	}
	if envelope.Error.Detail["reason"] != repositoryharness.QualityScopeModuleMissing {
		t.Fatalf("detail = %#v", envelope.Error.Detail)
	}
	if envelope.Error.Detail["model_calls"] != float64(0) {
		t.Fatalf("model_calls = %#v", envelope.Error.Detail["model_calls"])
	}
	if _, ok := envelope.Error.Detail["repair"]; ok {
		t.Fatalf("repository harness failure exposed quality tool repair guidance: %#v", envelope.Error.Detail)
	}
	if _, ok := envelope.Error.Detail["version_authority"]; ok {
		t.Fatalf("repository harness failure exposed quality tool version authority: %#v", envelope.Error.Detail)
	}
	if strings.Contains(envelope.Error.Message, qualityToolRepairEntry) {
		t.Fatalf("message exposed quality tool repair guidance: %q", envelope.Error.Message)
	}
}
