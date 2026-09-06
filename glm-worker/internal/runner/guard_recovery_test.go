package runner

import (
	"errors"
	"testing"
)

func TestIsRecoverableGuardFailure(t *testing.T) {
	refChange := GitRefChange{
		Name:  "refs/heads/bypass",
		After: &GitRefState{Name: "refs/heads/bypass", ObjectID: "2222222222222222222222222222222222222222"},
	}
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "blocked command without repository mutation",
			err:  &GitAuthorityGuardError{Stage: "blocked-command", Mutations: []string{"command:branch"}},
			want: true,
		},
		{
			name: "refs mutation without retained evidence remains unsafe",
			err:  &GitAuthorityGuardError{Stage: "after-call-mutation", Mutations: []string{"refs"}},
			want: false,
		},
		{
			name: "refs-only mutation with retained evidence is recoverable after repair",
			err: &GitAuthorityGuardError{
				Stage:           "after-call-mutation",
				Mutations:       []string{"refs"},
				RefBeforeDigest: "before",
				RefAfterDigest:  "after",
				RefChanges:      []GitRefChange{refChange},
			},
			want: true,
		},
		{
			name: "refs mutation joined with blocked attempt remains recoverable after repair",
			err: &GitAuthorityGuardError{
				Stage:           "after-call-mutation",
				Mutations:       []string{"command:branch", "refs"},
				RefBeforeDigest: "before",
				RefAfterDigest:  "after",
				RefChanges:      []GitRefChange{refChange},
			},
			want: true,
		},
		{
			name: "HEAD mutation stays non-recoverable",
			err: &GitAuthorityGuardError{
				Stage:           "after-call-mutation",
				Mutations:       []string{"HEAD", "refs"},
				RefBeforeDigest: "before",
				RefAfterDigest:  "after",
				RefChanges:      []GitRefChange{refChange},
			},
			want: false,
		},
		{
			name: "instruction surface also changed",
			err: errors.Join(
				&GitAuthorityGuardError{Stage: "blocked-command", Mutations: []string{"command:branch"}},
				&InstructionSurfaceGuardError{Stage: "after-call-mutation", ChangedPaths: []string{"AGENTS.md"}, Restored: true},
			),
			want: true,
		},
		{
			name: "restored instruction surface mutation only",
			err:  &InstructionSurfaceGuardError{Stage: "after-call-mutation", ChangedPaths: []string{"codex/AGENTS.md"}, Restored: true},
			want: true,
		},
		{
			name: "instruction surface restore failed",
			err:  &InstructionSurfaceGuardError{Stage: "restore-after-call", ChangedPaths: []string{"AGENTS.md"}, Cause: errors.New("restore failed")},
			want: false,
		},
		{
			name: "instruction surface baseline divergence",
			err:  &InstructionSurfaceGuardError{Stage: "before-call-mismatch", ChangedPaths: []string{"AGENTS.md/AGENTS.local.md"}},
			want: false,
		},
		{
			name: "git mutation joined with unrestored instruction surface change",
			err: errors.Join(
				&GitAuthorityGuardError{Stage: "blocked-command", Mutations: []string{"command:branch"}},
				&InstructionSurfaceGuardError{Stage: "verify-restored", ChangedPaths: []string{"AGENTS.md"}, Cause: errors.New("digest mismatch")},
			),
			want: false,
		},
		{
			name: "capture failure",
			err:  &GitAuthorityGuardError{Stage: "capture-after-call", Cause: errors.New("git unavailable")},
			want: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := IsRecoverableGuardFailure(test.err); got != test.want {
				t.Fatalf("IsRecoverableGuardFailure() = %v want %v", got, test.want)
			}
		})
	}
}

func TestIsPreCallGuardFailure(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "instruction baseline mismatch", err: &InstructionSurfaceGuardError{Stage: "before-call-mismatch", ChangedPaths: []string{"AGENTS.md/AGENTS.local.md"}}, want: true},
		{name: "instruction baseline read failure", err: &InstructionSurfaceGuardError{Stage: "read-task-baseline", Cause: errors.New("unreadable")}, want: true},
		{name: "instruction surface capture failure", err: &InstructionSurfaceGuardError{Stage: "capture-before-call", Cause: errors.New("walk failed")}, want: true},
		{name: "instruction symlink rejection", err: &InstructionSurfaceGuardError{Stage: "unsupported-instruction-symlink", ChangedPaths: []string{"AGENTS.local.md"}}, want: true},
		{name: "git snapshot capture failure", err: &GitAuthorityGuardError{Stage: "capture-before-call", Cause: errors.New("git failed")}, want: true},
		{name: "git command proxy preparation failure", err: &GitAuthorityGuardError{Stage: "prepare-command-proxy", Cause: errors.New("no temp dir")}, want: true},
		{name: "git claude wrapper preparation failure", err: &GitAuthorityGuardError{Stage: "prepare-claude-wrapper", Cause: errors.New("wrapper unwritable")}, want: true},
		{name: "restored after-call mutation", err: &InstructionSurfaceGuardError{Stage: "after-call-mutation", ChangedPaths: []string{"AGENTS.md"}, Restored: true}, want: false},
		{name: "after-call restore failure", err: &InstructionSurfaceGuardError{Stage: "restore-after-call", ChangedPaths: []string{"AGENTS.md"}, Cause: errors.New("restore failed")}, want: false},
		{name: "after-call verify failure", err: &InstructionSurfaceGuardError{Stage: "verify-restored", ChangedPaths: []string{"AGENTS.md"}, Cause: errors.New("digest mismatch")}, want: false},
		{name: "blocked git command after call", err: &GitAuthorityGuardError{Stage: "blocked-command", Mutations: []string{"command:branch"}}, want: false},
		{name: "git refs mutated after call", err: &GitAuthorityGuardError{Stage: "after-call-mutation", Mutations: []string{"refs"}}, want: false},
		{name: "parent baseline rotation failure", err: &InstructionSurfaceGuardError{Stage: "parent-rotation-no-change", Cause: errors.New("unchanged")}, want: false},
		{name: "plain error", err: errors.New("unrelated"), want: false},
		{name: "no error", err: nil, want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := IsPreCallGuardFailure(test.err); got != test.want {
				t.Fatalf("IsPreCallGuardFailure() = %v want %v", got, test.want)
			}
		})
	}
}

func TestIsPreCallGuardFailureText(t *testing.T) {
	tests := []struct {
		name string
		text string
		want bool
	}{
		{name: "instruction baseline mismatch", text: (&InstructionSurfaceGuardError{Stage: "before-call-mismatch", ChangedPaths: []string{"AGENTS.md/AGENTS.local.md"}}).Error(), want: true},
		{name: "instruction baseline read failure", text: (&InstructionSurfaceGuardError{Stage: "read-task-baseline", Cause: errors.New("unreadable")}).Error(), want: true},
		{name: "git snapshot capture failure", text: (&GitAuthorityGuardError{Stage: "capture-before-call", Cause: errors.New("git failed")}).Error(), want: true},
		{name: "git resolve failure", text: (&GitAuthorityGuardError{Stage: "resolve-git", Cause: errors.New("missing")}).Error(), want: true},
		{name: "restored after-call mutation", text: (&InstructionSurfaceGuardError{Stage: "after-call-mutation", ChangedPaths: []string{"AGENTS.md"}, Restored: true}).Error(), want: false},
		{name: "blocked git command", text: (&GitAuthorityGuardError{Stage: "blocked-command", Mutations: []string{"command:branch"}}).Error(), want: false},
		{name: "after-call restore failure", text: (&InstructionSurfaceGuardError{Stage: "restore-after-call", ChangedPaths: []string{"AGENTS.md"}, Cause: errors.New("restore failed")}).Error(), want: false},
		{name: "unrelated error text", text: "transient provider failure: timeout", want: false},
		{name: "guard prefix without stage", text: instructionSurfaceGuardErrorPrefix + ":", want: false},
		{name: "empty text", text: "", want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := IsPreCallGuardFailureText(test.text); got != test.want {
				t.Fatalf("IsPreCallGuardFailureText() = %v want %v", got, test.want)
			}
		})
	}
}
