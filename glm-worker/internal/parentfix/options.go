package parentfix

import (
	"errors"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type Options struct {
	Origin        string
	Cause         string
	AcceptedScope string
}

var ErrInvalidOptions = errors.New("invalid parent fix options")

func Extract(args []string) (Options, []string, error) {
	options := Options{}
	if len(args)%2 != 0 {
		return Options{}, nil, ErrInvalidOptions
	}
	remaining, err := extractSemanticPairs(&options, args)
	if err != nil || !validCombination(options) {
		return Options{}, nil, ErrInvalidOptions
	}
	return options, remaining, nil
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
	case "--cause":
		if options.Cause != "" || !state.ValidParentCause(value) {
			return true, ErrInvalidOptions
		}
		options.Cause = value
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
	if options.Origin == state.ParentOriginCodexReview {
		return options.Cause != ""
	}
	return true
}
