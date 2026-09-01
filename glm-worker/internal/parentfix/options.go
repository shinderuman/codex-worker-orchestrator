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
	remaining := make([]string, 0, len(args))
	seenOrigin := false
	seenScope := false

	for index := 0; index < len(args); {
		switch args[index] {
		case "--approval-only":
			if options.ApprovalOnly {
				return Options{}, nil, ErrInvalidOptions
			}
			options.ApprovalOnly = true
			index++
		case "--origin":
			if seenOrigin || index+1 >= len(args) || !state.ValidParentOrigin(args[index+1]) {
				return Options{}, nil, ErrInvalidOptions
			}
			seenOrigin = true
			options.Origin = args[index+1]
			index += 2
		case "--accepted-scope":
			if seenScope || index+1 >= len(args) || args[index+1] != "current-diff" {
				return Options{}, nil, ErrInvalidOptions
			}
			seenScope = true
			options.AcceptedScope = args[index+1]
			index += 2
		default:
			remaining = append(remaining, args[index])
			index++
		}
	}

	if options.ApprovalOnly && (options.AcceptedScope != "current-diff" || options.Origin != "") {
		return Options{}, nil, ErrInvalidOptions
	}
	return options, remaining, nil
}
