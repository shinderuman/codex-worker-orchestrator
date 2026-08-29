package runner

import "errors"

func IsRecoverableGuardFailure(err error) bool {
	var gitErr *GitAuthorityGuardError
	hasGitFailure := errors.As(err, &gitErr)
	if hasGitFailure && gitErr.Stage != "blocked-command" {
		return false
	}
	var instructionErr *InstructionSurfaceGuardError
	hasInstructionFailure := errors.As(err, &instructionErr)
	if hasInstructionFailure && !isRestoredInstructionSurfaceMutation(*instructionErr) {
		return false
	}
	return hasGitFailure || hasInstructionFailure
}

func isRestoredInstructionSurfaceMutation(err InstructionSurfaceGuardError) bool {
	return err.Stage == "after-call-mutation" && err.Restored
}
