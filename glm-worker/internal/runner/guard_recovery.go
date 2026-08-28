package runner

import "errors"

func IsRecoverableGuardFailure(err error) bool {
	var gitErr *GitAuthorityGuardError
	if !errors.As(err, &gitErr) || gitErr.Stage != "blocked-command" {
		return false
	}

	var instructionErr *InstructionSurfaceGuardError
	return !errors.As(err, &instructionErr)
}
