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

	remaining := make([]string, 0, len(pairs))
	seenOrigin := false
	seenScope := false
	for index := 0; index < len(pairs); index += 2 {
		name := pairs[index]
		value := pairs[index+1]
		switch name {
		case "--origin":
			if seenOrigin || !state.ValidParentOrigin(value) {
				return Options{}, nil, ErrInvalidOptions
			}
			seenOrigin = true
			options.Origin = value
		case "--accepted-scope":
			if seenScope || value != "current-diff" {
				return Options{}, nil, ErrInvalidOptions
			}
			seenScope = true
			options.AcceptedScope = value
		default:
			remaining = append(remaining, name, value)
		}
	}

	if options.ApprovalOnly && (options.AcceptedScope != "current-diff" || options.Origin != "") {
		return Options{}, nil, ErrInvalidOptions
	}
	return options, remaining, nil
}
