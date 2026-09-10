package runner

import (
	"errors"
	"strings"
)

const (
	instructionSurfaceGuardErrorPrefix = "repository instruction surface guard failed"
	gitAuthorityGuardErrorPrefix       = "git authority guard failed"
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
	if err.Stage != guardStageAfterCallMutation || err.RefBeforeDigest == "" || err.RefAfterDigest == "" || err.RefBeforeDigest == err.RefAfterDigest || len(err.RefChanges) == 0 {
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
	return err.Stage == guardStageAfterCallMutation && err.Restored
}

func IsPreCallGuardFailure(err error) bool {
	var instructionErr *InstructionSurfaceGuardError
	if errors.As(err, &instructionErr) {
		return isPreCallInstructionGuardStage(instructionErr.Stage)
	}
	var gitErr *GitAuthorityGuardError
	if errors.As(err, &gitErr) {
		return isPreCallGitGuardStage(gitErr.Stage)
	}
	return false
}

func IsPreCallGuardFailureText(text string) bool {
	if stage, ok := guardErrorStageFromText(text, instructionSurfaceGuardErrorPrefix); ok {
		return isPreCallInstructionGuardStage(stage)
	}
	stage, ok := guardErrorStageFromText(text, gitAuthorityGuardErrorPrefix)
	if !ok {
		return false
	}
	return isPreCallGitGuardStage(stage)
}

func SameGuardFailureFamilyText(first, second string) bool {
	firstFamily, ok := guardFailureFamilyFromText(first)
	if !ok {
		return false
	}
	secondFamily, ok := guardFailureFamilyFromText(second)
	return ok && firstFamily == secondFamily
}

func guardFailureFamilyFromText(text string) (string, bool) {
	if _, ok := guardErrorStageFromText(text, instructionSurfaceGuardErrorPrefix); ok {
		return instructionSurfaceGuardErrorPrefix, true
	}
	if _, ok := guardErrorStageFromText(text, gitAuthorityGuardErrorPrefix); ok {
		return gitAuthorityGuardErrorPrefix, true
	}
	return "", false
}

func guardErrorStageFromText(text string, prefix string) (string, bool) {
	rest, found := strings.CutPrefix(text, prefix+":")
	if !found {
		return "", false
	}
	stage, _, _ := strings.Cut(strings.TrimPrefix(rest, " "), ":")
	return stage, true
}

func isPreCallInstructionGuardStage(stage string) bool {
	switch stage {
	case "capture-before-call",
		"unsupported-instruction-symlink",
		"read-task-identity",
		"read-task-baseline",
		"before-call-mismatch",
		"persist-task-baseline":
		return true
	default:
		return false
	}
}

func isPreCallGitGuardStage(stage string) bool {
	switch stage {
	case "resolve-git",
		"capture-before-call",
		"resolve-metadata",
		"prepare-command-proxy",
		"prepare-claude-wrapper":
		return true
	default:
		return false
	}
}
