package parentactioncmd

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repolock"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type publicationFindingOptions struct {
	candidateOID string
	snapshotID   string
	origin       string
	cause        string
}

type publicationFindingOutput struct {
	Status         string                               `json:"status"`
	Finding        state.PublicationInvalidatingFinding `json:"finding"`
	RequiredAction state.ParentAction                   `json:"required_action"`
	AllowedActions []state.ParentAction                 `json:"allowed_actions"`
}

const (
	actionRecordPublicationFinding = "record-publication-finding"
	publicationFindingUsage        = "usage: glm-parent-action record-publication-finding --candidate-oid <oid> --snapshot-id <snapshot-id> [--origin <origin>] [--cause <cause>]"
)

func executeRecordPublicationFinding(cfg config.AppConfig, args []string, stdout io.Writer) error {
	options, err := parsePublicationFindingOptions(args)
	if err != nil {
		return err
	}
	if err := persistParentCodexIdentity(cfg); err != nil {
		return err
	}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		return err
	}
	lock, err := repolock.Acquire(st.LockPath())
	if err != nil {
		return err
	}
	defer func() { _ = lock.Close() }()

	finding, err := st.RecordPublicationInvalidatingFinding(options.candidateOID, options.snapshotID, options.origin, options.cause)
	if err != nil {
		return err
	}
	plan, err := st.ParentActionPlan()
	if err != nil {
		return err
	}
	if plan.RequiredAction != state.ParentActionReopen || !plan.Allows(state.ParentActionReopen) || plan.Allows(state.ParentActionComplete) {
		return fmt.Errorf("recorded publication finding did not force machine-required reopen")
	}
	return json.NewEncoder(stdout).Encode(publicationFindingOutput{
		Status:         "recorded",
		Finding:        finding,
		RequiredAction: plan.RequiredAction,
		AllowedActions: plan.AllowedActions,
	})
}

func parsePublicationFindingOptions(args []string) (publicationFindingOptions, error) {
	if len(args) < 5 || args[0] != actionRecordPublicationFinding {
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
	if options.candidateOID == "" || options.snapshotID == "" {
		return publicationFindingOptions{}, fmt.Errorf("%s", publicationFindingUsage)
	}
	return options, nil
}

func setPublicationFindingOption(options *publicationFindingOptions, name, value string) error {
	var target *string
	switch name {
	case "--candidate-oid":
		target = &options.candidateOID
	case "--snapshot-id":
		target = &options.snapshotID
	case "--origin":
		target = &options.origin
	case "--cause":
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
