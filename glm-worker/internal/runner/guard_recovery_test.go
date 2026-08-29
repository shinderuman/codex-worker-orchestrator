package runner

import (
	"errors"
	"testing"
)

func TestIsRecoverableGuardFailure(t *testing.T) {
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
			name: "protected repository mutation",
			err:  &GitAuthorityGuardError{Stage: "after-call-mutation", Mutations: []string{"refs"}},
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
