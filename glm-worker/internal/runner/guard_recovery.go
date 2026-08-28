package runner

import "errors"

// IsRecoverableGuardFailure reports whether a model call was rejected only by
// the pre-execution Git command guard. The protected repository snapshot did
// not mutate in this case, so the model session must still be discarded but
// the enclosing task may be recoverable after the parent fixes the guard or
// its policy.
func IsRecoverableGuardFailure(err error) bool {
	var gitErr *GitAuthorityGuardError
	if !errors.As(err, &gitErr) || gitErr.Stage != "blocked-command" {
		return false
	}

	var instructionErr *InstructionSurfaceGuardError
	return !errors.As(err, &instructionErr)
}
