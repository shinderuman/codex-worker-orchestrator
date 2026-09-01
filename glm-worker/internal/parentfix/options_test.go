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
		options, remaining, err := Extract([]string{"--origin", origin})
		if err != nil {
			t.Fatalf("origin %q: %v", origin, err)
		}
		if options.Origin != origin || options.AcceptedScope != "" || options.ApprovalOnly || len(remaining) != 0 {
			t.Fatalf("origin %q options = %#v remaining=%v", origin, options, remaining)
		}
	}

	options, remaining, err := Extract([]string{"--accepted-scope", "current-diff", "--approval-only"})
	if err != nil {
		t.Fatal(err)
	}
	if options.AcceptedScope != "current-diff" || !options.ApprovalOnly || options.Origin != "" || len(remaining) != 0 {
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
		{"--accepted-scope"},
		{"--accepted-scope", "other"},
		{"--accepted-scope", "current-diff", "--accepted-scope", "current-diff"},
		{"--approval-only"},
		{"--approval-only", "--approval-only", "--accepted-scope", "current-diff"},
		{"--approval-only", "--accepted-scope", "current-diff", "--origin", state.ParentOriginCodexReview},
	} {
		if _, _, err := Extract(args); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("Extract(%v) error = %v, want ErrInvalidOptions", args, err)
		}
	}
}
