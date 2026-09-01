package parentfix

import (
	"errors"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type Options struct {
	Origin        string
	AcceptedScope string
	ApprovalOnly  bool
}

var ErrInvalidOptions = errors.New("invalid parent fix options")

func Extract(args []string) (Options, []string, error) {
	options, pairs, err := extractApprovalOnly(args)
	if err != nil {
		return Options{}, nil, err
	}
	remaining, err := extractSemanticPairs(&options, pairs)
	if err != nil || !validCombination(options) {
		return Options{}, nil, ErrInvalidOptions
	}
	return options, remaining, nil
}

func extractApprovalOnly(args []string) (Options, []string, error) {
	var options Options
	pairs := make([]string, 0, len(args))
	for _, arg := range args {
		if arg != "--approval-only" {
			pairs = append(pairs, arg)
			continue
		}
		if options.ApprovalOnly {
			return Options{}, nil, ErrInvalidOptions
		}
		options.ApprovalOnly = true
	}
	if len(pairs)%2 != 0 {
		return Options{}, nil, ErrInvalidOptions
	}
	return options, pairs, nil
}

func extractSemanticPairs(options *Options, pairs []string) ([]string, error) {
	remaining := make([]string, 0, len(pairs))
	for index := 0; index < len(pairs); index += 2 {
		handled, err := applySemanticPair(options, pairs[index], pairs[index+1])
		if err != nil {
			return nil, err
		}
		if !handled {
			remaining = append(remaining, pairs[index], pairs[index+1])
		}
	}
	return remaining, nil
}

func applySemanticPair(options *Options, name, value string) (bool, error) {
	switch name {
	case "--origin":
		if options.Origin != "" || !state.ValidParentOrigin(value) {
			return true, ErrInvalidOptions
		}
		options.Origin = value
		return true, nil
	case "--accepted-scope":
		if options.AcceptedScope != "" || value != "current-diff" {
			return true, ErrInvalidOptions
		}
		options.AcceptedScope = value
		return true, nil
	default:
		return false, nil
	}
}

func validCombination(options Options) bool {
	return !options.ApprovalOnly || (options.AcceptedScope == "current-diff" && options.Origin == "")
}
