package parentfix

import (
	"errors"
	"reflect"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestExtractAcceptsCurrentFixGrammar(t *testing.T) {
	for _, origin := range []string{
		state.ParentOriginCodexReview,
		state.ParentOriginGLMReviewer,
		state.ParentOriginUserAmendment,
		state.ParentOriginExternalReview,
		state.ParentOriginMetadataRepair,
	} {
		args := []string{"--origin", origin}
		if origin == state.ParentOriginCodexReview {
			args = append(args, "--cause", state.ParentCauseWorker)
		}
		options, remaining, err := Extract(args)
		if err != nil {
			t.Fatalf("origin %q: %v", origin, err)
		}
		wantCause := ""
		if origin == state.ParentOriginCodexReview {
			wantCause = state.ParentCauseWorker
		}
		if options.Origin != origin || options.Cause != wantCause ||
			options.AcceptedScope != "" || options.ApprovalOnly || len(remaining) != 0 {
			t.Fatalf("origin %q options = %#v remaining=%v", origin, options, remaining)
		}
	}

	for _, cause := range []string{
		state.ParentCauseParentOrchestration,
		state.ParentCauseRequirementPreservation,
		state.ParentCauseWorker,
		state.ParentCauseReviewer,
		state.ParentCauseSolGate,
		state.ParentCauseProductionWiring,
		state.ParentCauseTestScenario,
		state.ParentCauseCrossCuttingInvariant,
		state.ParentCauseUnknown,
	} {
		options, _, err := Extract([]string{"--origin", state.ParentOriginGLMReviewer, "--cause", cause})
		if err != nil {
			t.Fatalf("cause %q: %v", cause, err)
		}
		if options.Cause != cause {
			t.Fatalf("cause %q options = %#v", cause, options)
		}
	}

	options, remaining, err := Extract([]string{"--accepted-scope", "current-diff", "--approval-only"})
	if err != nil {
		t.Fatal(err)
	}
	if options.AcceptedScope != "current-diff" || !options.ApprovalOnly || options.Origin != "" || options.Cause != "" || len(remaining) != 0 {
		t.Fatalf("approval options = %#v remaining=%v", options, remaining)
	}
}

func TestExtractLeavesTransportOptionsForCaller(t *testing.T) {
	args := []string{"--sha256", "abcd", "--accepted-scope", "current-diff"}
	options, remaining, err := Extract(args)
	if err != nil {
		t.Fatal(err)
	}
	if options.AcceptedScope != "current-diff" {
		t.Fatalf("options = %#v", options)
	}
	if !reflect.DeepEqual(remaining, []string{"--sha256", "abcd"}) {
		t.Fatalf("remaining = %v", remaining)
	}
}

func TestExtractRejectsInvalidFixGrammar(t *testing.T) {
	for _, args := range [][]string{
		{"--origin"},
		{"--origin", "unknown"},
		{"--origin", state.ParentOriginCodexReview, "--origin", state.ParentOriginGLMReviewer},
		{"--origin", state.ParentOriginCodexReview},
		{"--accepted-scope"},
		{"--accepted-scope", "other"},
		{"--accepted-scope", "current-diff", "--accepted-scope", "current-diff"},
		{"--approval-only"},
		{"--approval-only", "--approval-only", "--accepted-scope", "current-diff"},
		{"--approval-only", "--accepted-scope", "current-diff", "--origin", state.ParentOriginCodexReview},
		{"--approval-only", "--accepted-scope", "current-diff", "--cause", state.ParentCauseWorker},
		{"--cause"},
		{"--cause", "vibe"},
		{"--cause", state.ParentCauseWorker, "--cause", state.ParentCauseReviewer},
		{"--origin", state.ParentOriginGLMReviewer, "--cause", "legacy-layer"},
	} {
		if _, _, err := Extract(args); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("Extract(%v) error = %v, want ErrInvalidOptions", args, err)
		}
	}
}
