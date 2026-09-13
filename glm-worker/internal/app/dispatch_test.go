package app

import "testing"

func TestCommandDispatchOwnersCoverAllModes(t *testing.T) {
	for mode := ModeNewTask; mode <= ModeCodexWakeResponse; mode++ {
		if _, err := commandDispatchOwnerFor(mode); err != nil {
			t.Fatalf("mode %d has no dispatch owner: %v", mode, err)
		}
	}
}

func TestCommandDispatchOwnersSeparateRuntimeAndReadOnly(t *testing.T) {
	for _, mode := range []CommandMode{ModeStop, ModeCodexWakePlan, ModeCodexWakeResponse} {
		owner, err := commandDispatchOwnerFor(mode)
		if err != nil {
			t.Fatal(err)
		}
		if owner != dispatchRuntimeControl {
			t.Fatalf("mode %d owner = %d, want runtime control", mode, owner)
		}
	}

	for _, mode := range []CommandMode{
		ModeStatus,
		ModeHandoff,
		ModeStats,
		ModeWatch,
		ModeTimeline,
		ModeEvalAB,
		ModeRepoSearch,
		ModeEvidence,
		ModeReviewGap,
	} {
		owner, err := commandDispatchOwnerFor(mode)
		if err != nil {
			t.Fatal(err)
		}
		if owner != dispatchReadOnly {
			t.Fatalf("mode %d owner = %d, want read-only", mode, owner)
		}
	}
}

func TestCommandDispatchOwnersSeparateStateLockedAndWorkflow(t *testing.T) {
	cases := map[CommandMode]commandDispatchOwner{
		ModeVerifyAutoResume: dispatchStateCommand,
		ModeInstallSmoke:     dispatchStateCommand,
		ModeReset:            dispatchLockedMutation,
		ModeAccept:           dispatchLockedMutation,
		ModePark:             dispatchLockedMutation,
		ModeNewTask:          dispatchWorkflow,
		ModeDecision:         dispatchWorkflow,
		ModeResume:           dispatchWorkflow,
	}
	for mode, want := range cases {
		got, err := commandDispatchOwnerFor(mode)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("mode %d owner = %d, want %d", mode, got, want)
		}
	}
}

func TestCodexWakeCommandsUseCanonicalParserRegistry(t *testing.T) {
	for _, name := range []string{"--codex-wake-plan", "--codex-wake-response-stdin"} {
		if commandParsers[name] == nil {
			t.Fatalf("canonical command parser missing %q", name)
		}
	}
}
