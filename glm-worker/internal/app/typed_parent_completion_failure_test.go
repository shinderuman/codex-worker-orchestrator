package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type typedCompletionFailureBoundary struct {
	cfg                 config.AppConfig
	runtime             *state.StateStore
	store               *controller.Store
	candidate           controller.AcceptedCandidate
	handoff             parentHandoffOutput
	flag                string
	code                int
	output, diagnostics string
}

func typedCompletionFailureFixture(t *testing.T, native bool) typedCompletionFailureBoundary {
	t.Helper()
	binary := typedCompletionWorkerBinary(t)
	cfg, runtime, shim := typedCompletionWorkflowFixture(t, binary, true)
	flag := filepath.Join(t.TempDir(), "fail-boundary")
	if err := os.WriteFile(flag, []byte("fail"), 0600); err != nil {
		t.Fatal(err)
	}
	if native {
		t.Setenv("GLM_TYPED_TEST_INSTALL_FAILURE", flag)
	} else {
		path := filepath.Join(shim, "go")
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		failure := fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = test ] && [ -f '%s' ]; then\ncat <<'EVENTS'\n%s\nEVENTS\nexit 3\nfi\n", flag, qualityGateShimFailEvents)
		if err := os.WriteFile(path, []byte(failure+strings.TrimPrefix(string(body), "#!/bin/sh\n")), 0755); err != nil {
			t.Fatal(err)
		}
	}
	handoff := executeParentHandoff(t, cfg)
	code, output, diagnostics := runTypedPublicationCLI(t, cfg, handoff.RequiredActionParameters, typedSemanticSlotValues(t, handoff.RequiredActionParameters, true))
	store, err := controller.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	head, err := store.LoadHead()
	if err != nil || head.AcceptedCandidateRef == nil {
		t.Fatalf("failure fixture has no fixed candidate: %v", err)
	}
	candidate, err := store.LoadAcceptedCandidate(*head.AcceptedCandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	return typedCompletionFailureBoundary{cfg, runtime, store, candidate, executeParentHandoff(t, cfg), flag, code, output, diagnostics}
}

func TestTypedCompletionRetriesFixedSource(t *testing.T) {
	for _, native := range []bool{false, true} {
		t.Run(fmt.Sprintf("native-install-%t", native), func(t *testing.T) {
			f := typedCompletionFailureFixture(t, native)
			if native && f.code == 0 {
				t.Fatal("native install failure was not returned")
			}
			if !native {
				if f.code != 0 {
					t.Fatal(f.diagnostics)
				}
				var result struct {
					Terminal struct {
						Status string `json:"status"`
					} `json:"terminal"`
				}
				if err := json.Unmarshal([]byte(f.output), &result); err != nil {
					t.Fatal(err)
				}
				if result.Terminal.Status != "blocked" {
					t.Fatal("failed validation was treated as complete")
				}
			}
			head, err := f.store.LoadHead()
			if err != nil {
				t.Fatal(err)
			}
			if f.candidate.BundleCheckRef != nil || head.IntegrationTip != f.candidate.BaseOID {
				t.Fatal("failure froze a Bundle or promoted the candidate")
			}
			var command controller.PublicationParentCommand
			if err := json.Unmarshal([]byte(f.handoff.RequiredActionParameters["controller_command_json"]), &command); err != nil {
				t.Fatal(err)
			}
			expected := controller.PublicationOperationValidationFailure
			if native {
				expected = controller.PublicationOperationInstall
			}
			if command.Action != expected || command.CandidateID != f.candidate.CandidateID || command.SourceOID != f.candidate.CommitOID {
				t.Fatal("failure has no source-bound typed disposition")
			}
			if err := os.Remove(f.flag); err != nil {
				t.Fatal(err)
			}
			if code, _, diagnostic := runTypedPublicationCLI(t, f.cfg, f.handoff.RequiredActionParameters, map[string]string{"failure_disposition": "retry"}); code != 0 {
				t.Fatal(diagnostic)
			}
			assertTypedCompletionTerminalState(t, f.cfg, f.runtime)
			head, err = f.store.LoadHead()
			if err != nil {
				t.Fatal(err)
			}
			if head.IntegrationTip != f.candidate.CommitOID {
				t.Fatal("retry replaced the fixed candidate")
			}
		})
	}
}

func TestTypedCompletionReentersWithoutOldProof(t *testing.T) {
	f := typedCompletionFailureFixture(t, false)
	if f.code != 0 {
		t.Fatal(f.diagnostics)
	}
	var command controller.PublicationParentCommand
	if err := json.Unmarshal([]byte(f.handoff.RequiredActionParameters["controller_command_json"]), &command); err != nil {
		t.Fatal(err)
	}
	if command.Action != controller.PublicationOperationValidationFailure || command.SourceOID != f.candidate.CommitOID {
		t.Fatal("validation failure lost its candidate source")
	}
	if code, _, diagnostic := runTypedPublicationCLI(t, f.cfg, f.handoff.RequiredActionParameters, map[string]string{"failure_disposition": "reenter"}); code != 0 {
		t.Fatal(diagnostic)
	}
	head, err := f.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	invalid, err := f.store.LoadAcceptedCandidate(*head.AcceptedCandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	if invalid.EvidenceValid || invalid.CommitOID != f.candidate.CommitOID {
		t.Fatal("source reentry retained usable proof or discarded the original source")
	}
	handoff := executeParentHandoff(t, f.cfg)
	if handoff.RequiredAction == nil || *handoff.RequiredAction != controllerExecutionSurface || handoff.RequiredActionParameters["controller_command_json"] == "" {
		t.Fatal("reentry did not offer the existing typed materialization owner")
	}
}
