package app

import (
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestStagedFixUsesCanonicalSemanticOptionsAlongsideTransport(t *testing.T) {
	t.Parallel()
	command, err := ParseCommand(stagedTestCommandArgs("fix", "--origin", state.ParentOriginExternalReview,
		"--accepted-scope", "current-diff"))
	if err != nil {
		t.Fatal(err)
	}
	if command.Mode != ModeFix || command.ParentStageToken != stagedTestToken || command.SHA256 != stagedTestDigest {
		t.Fatalf("transport command = %#v", command)
	}
	if command.Origin != state.ParentOriginExternalReview || command.AcceptedScope != "current-diff" {
		t.Fatalf("semantic command = %#v", command)
	}
}

func TestStagedFixRejectsInvalidCanonicalSemanticOptionsDuringParsing(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		stagedTestCommandArgs("fix", "--accepted-scope", "other"),
		stagedTestCommandArgs("fix", "--accepted-scope", "current-diff", "--approval-only"),
		stagedTestCommandArgs("fix", "--origin", state.ParentOriginCodexReview, "--origin", state.ParentOriginGLMReviewer),
		stagedTestCommandArgs("fix", "--unknown", "value"),
		stagedTestCommandArgs("fix", "--sha256", "--origin", state.ParentOriginCodexReview, strings.Repeat("a", 64)),
	} {
		if _, err := ParseCommand(args); err == nil {
			t.Fatalf("ParseCommand(%v) unexpectedly succeeded", args)
		}
	}
}
