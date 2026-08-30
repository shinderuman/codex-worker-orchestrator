package runner

import (
	"errors"
	"strings"
)

func IsRecoverableGuardFailure(err error) bool {
	var gitErr *GitAuthorityGuardError
	hasGitFailure := errors.As(err, &gitErr)
	if hasGitFailure && !isRecoverableGitAuthorityFailure(*gitErr) {
		return false
	}
	var instructionErr *InstructionSurfaceGuardError
	hasInstructionFailure := errors.As(err, &instructionErr)
	if hasInstructionFailure && !isRestoredInstructionSurfaceMutation(*instructionErr) {
		return false
	}
	return hasGitFailure || hasInstructionFailure
}

func isRecoverableGitAuthorityFailure(err GitAuthorityGuardError) bool {
	if err.Stage == "blocked-command" {
		return true
	}
	if err.Stage != "after-call-mutation" || err.RefBeforeDigest == "" || err.RefAfterDigest == "" || err.RefBeforeDigest == err.RefAfterDigest || len(err.RefChanges) == 0 {
		return false
	}
	for _, mutation := range err.Mutations {
		if mutation == "refs" || strings.HasPrefix(mutation, "command:") {
			continue
		}
		return false
	}
	return true
}

func isRestoredInstructionSurfaceMutation(err InstructionSurfaceGuardError) bool {
	return err.Stage == "after-call-mutation" && err.Restored
}
