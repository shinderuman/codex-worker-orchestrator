package parentactioncmd

import (
	"fmt"
	"io"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type publicationFindingOptions struct {
	origin string
	cause  string
}

type publicationFindingOutput struct {
	Status         string                                `json:"status"`
	Finding        state.PublicationInvalidatingFinding `json:"finding"`
	RequiredAction state.ParentAction                    `json:"required_action"`
	AllowedActions []state.ParentAction                  `json:"allowed_actions"`
}

const (
	actionRecordPublicationFinding = "record-publication-finding"
	publicationFindingUsage        = "usage: glm-parent-action record-publication-finding [--origin <origin>] [--cause <cause>]"
)

func executeRecordPublicationFinding(cfg config.AppConfig, args []string, stdout io.Writer) error {
	if _, err := parsePublicationFindingOptions(args); err != nil {
		return err
	}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		return err
	}
	if st.TaskStatus() != state.TaskStatusAwaitingParentCompletion {
		return fmt.Errorf("publication invalidating finding requires %s, got %s", state.TaskStatusAwaitingParentCompletion, st.TaskStatus())
	}
	completion, err := st.CurrentParentCompletionOutcome()
	if err != nil {
		return err
	}
	if completion == nil || completion.Terminal != state.SessionRotationTerminalAccept {
		return fmt.Errorf("publication invalidating finding requires an accepted parent completion outcome")
	}
	if _, err := st.LoadPublicationCandidate(); err != nil {
		return fmt.Errorf("publication invalidating finding requires current publication candidate: %w", err)
	}
	return fmt.Errorf("publication finding has no machine-owned same-task scope evidence; %s", sameTaskScopeRegistrationHint)
}

func parsePublicationFindingOptions(args []string) (publicationFindingOptions, error) {
	if len(args) == 0 || args[0] != actionRecordPublicationFinding {
		return publicationFindingOptions{}, fmt.Errorf("%s", publicationFindingUsage)
	}
	var options publicationFindingOptions
	for i := 1; i < len(args); i += 2 {
		if i+1 >= len(args) {
			return publicationFindingOptions{}, fmt.Errorf("%s", publicationFindingUsage)
		}
		if err := setPublicationFindingOption(&options, args[i], args[i+1]); err != nil {
			return publicationFindingOptions{}, err
		}
	}
	if options.origin == state.ParentOriginCodexReview && options.cause == "" {
		return publicationFindingOptions{}, fmt.Errorf("%s", publicationFindingUsage)
	}
	return options, nil
}

func setPublicationFindingOption(options *publicationFindingOptions, name, value string) error {
	var target *string
	switch name {
	case "--origin":
		if !state.ValidParentOrigin(value) {
			return fmt.Errorf("%s", publicationFindingUsage)
		}
		target = &options.origin
	case "--cause":
		if !state.ValidParentCause(value) {
			return fmt.Errorf("%s", publicationFindingUsage)
		}
		target = &options.cause
	default:
		return fmt.Errorf("%s", publicationFindingUsage)
	}
	if *target != "" {
		return fmt.Errorf("%s", publicationFindingUsage)
	}
	*target = value
	return nil
}
