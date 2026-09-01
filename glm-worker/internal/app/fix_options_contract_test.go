package app

import (
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestFixStdinUsesCanonicalSemanticOptionsAlongsideTransport(t *testing.T) {
	digest := strings.Repeat("a", 64)
	command, err := ParseCommand([]string{
		"--fix-stdin", "12",
		"--sha256", digest,
		"--origin", state.ParentOriginExternalReview,
		"--accepted-scope", "current-diff",
	})
	if err != nil {
		t.Fatal(err)
	}
	if command.Mode != ModeFix || command.StdinBytes != 12 || command.SHA256 != digest {
		t.Fatalf("transport command = %#v", command)
	}
	if command.Origin != state.ParentOriginExternalReview || command.AcceptedScope != "current-diff" || command.ApprovalOnly {
		t.Fatalf("semantic command = %#v", command)
	}

	approval, err := ParseCommand([]string{
		"--fix-stdin", "12",
		"--accepted-scope", "current-diff",
		"--approval-only",
	})
	if err != nil {
		t.Fatal(err)
	}
	if approval.AcceptedScope != "current-diff" || !approval.ApprovalOnly || approval.Origin != "" {
		t.Fatalf("approval command = %#v", approval)
	}
}

func TestFixStdinRejectsInvalidCanonicalSemanticOptionsDuringParsing(t *testing.T) {
	for _, args := range [][]string{
		{"--fix-stdin", "12", "--approval-only"},
		{"--fix-stdin", "12", "--accepted-scope", "other"},
		{"--fix-stdin", "12", "--accepted-scope", "current-diff", "--approval-only", "--origin", state.ParentOriginCodexReview},
		{"--fix-stdin", "12", "--origin", state.ParentOriginCodexReview, "--origin", state.ParentOriginGLMReviewer},
		{"--fix-stdin", "12", "--unknown", "value"},
	} {
		if _, err := ParseCommand(args); err == nil {
			t.Fatalf("ParseCommand(%v) unexpectedly succeeded", args)
		}
	}
}
