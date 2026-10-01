package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryharness"
)

type controllerSemanticAction string

type controllerSemanticCommand struct {
	Action            controllerSemanticAction            `json:"action"`
	Observation       *controller.FindingObservationInput `json:"observation,omitempty"`
	AttemptID         string                              `json:"attempt_id,omitempty"`
	ProjectSnapshotID string                              `json:"project_snapshot_id,omitempty"`
	FindingID         string                              `json:"finding_id,omitempty"`
	Decision          *controller.FindingDecision         `json:"decision,omitempty"`
	EpisodeID         string                              `json:"episode_id,omitempty"`
	EpisodeRevision   uint64                              `json:"episode_revision,omitempty"`
}

type controllerSemanticOutput struct {
	Action   controllerSemanticAction            `json:"action"`
	Finding  *controller.FindingRecord            `json:"finding,omitempty"`
	Result   *controller.FindingDispositionResult `json:"result,omitempty"`
	Schedule *controller.EpisodeScheduleResult    `json:"schedule,omitempty"`
}

const (
	controllerSemanticObserveLive     controllerSemanticAction = "observe-live-finding"
	controllerSemanticObserveTerminal controllerSemanticAction = "observe-terminal-finding"
	controllerSemanticResolve         controllerSemanticAction = "resolve-finding"
	controllerSemanticSchedule        controllerSemanticAction = "schedule-episode"
)

func runControllerSemantic(
	args []string,
	loadConfig func() (config.AppConfig, error),
	stdin io.Reader,
	stdout io.Writer,
) (bool, error) {
	if len(args) != 2 || args[0] != controllerAuthorityFlag || args[1] != "controller-semantic" {
		return false, nil
	}
	cfg, err := loadConfig()
	if err != nil {
		return true, err
	}
	store, err := openControllerSemanticStore(cfg)
	if err != nil {
		return true, err
	}
	command, err := decodeControllerSemanticCommand(stdin)
	if err != nil {
		return true, err
	}
	output, err := executeControllerSemantic(cfg, store, command)
	if err != nil {
		return true, err
	}
	return true, writeValidatedMachineJSON(stdout, output)
}

func openControllerSemanticStore(cfg config.AppConfig) (*controller.Store, error) {
	decision, err := repositoryharness.Evaluate(cfg.RepoRoot)
	if err != nil {
		return nil, fmt.Errorf("evaluate repository controller semantic action: %w", err)
	}
	if !decision.Active {
		return nil, fmt.Errorf("repository controller semantic action requires active repository harness")
	}
	exists, err := controller.Exists(cfg)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("repository controller semantic action requires existing controller authority")
	}
	return controller.Open(cfg)
}

func decodeControllerSemanticCommand(input io.Reader) (controllerSemanticCommand, error) {
	decoder := json.NewDecoder(input)
	decoder.DisallowUnknownFields()
	var command controllerSemanticCommand
	if err := decoder.Decode(&command); err != nil {
		return controllerSemanticCommand{}, fmt.Errorf("decode controller semantic command: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return controllerSemanticCommand{}, fmt.Errorf("controller semantic command contains trailing JSON")
	}
	if command.Action == "" {
		return controllerSemanticCommand{}, fmt.Errorf("controller semantic action is required")
	}
	return command, nil
}

func executeControllerSemantic(
	cfg config.AppConfig,
	store *controller.Store,
	command controllerSemanticCommand,
) (controllerSemanticOutput, error) {
	switch command.Action {
	case controllerSemanticObserveLive:
		return executeControllerSemanticObserveLive(cfg, store, command)
	case controllerSemanticObserveTerminal:
		return executeControllerSemanticObserveTerminal(store, command)
	case controllerSemanticResolve:
		return executeControllerSemanticResolve(store, command)
	case controllerSemanticSchedule:
		return executeControllerSemanticSchedule(store, command)
	default:
		return controllerSemanticOutput{}, fmt.Errorf("unsupported controller semantic action %q", command.Action)
	}
}

func executeControllerSemanticObserveLive(
	cfg config.AppConfig,
	store *controller.Store,
	command controllerSemanticCommand,
) (controllerSemanticOutput, error) {
	if command.Observation == nil {
		return controllerSemanticOutput{}, fmt.Errorf("live finding observation is required")
	}
	admission, err := currentControllerSemanticAdmission(cfg, store)
	if err != nil {
		return controllerSemanticOutput{}, err
	}
	finding, err := store.ObserveFinding(admission, *command.Observation)
	if err != nil {
		return controllerSemanticOutput{}, err
	}
	return controllerSemanticOutput{Action: command.Action, Finding: &finding}, nil
}

func currentControllerSemanticAdmission(cfg config.AppConfig, store *controller.Store) (controller.Admission, error) {
	head, err := store.LoadHead()
	if err != nil {
		return controller.Admission{}, err
	}
	authority, err := controller.MutationAuthorityFromHead(head)
	if err != nil {
		return controller.Admission{}, err
	}
	identity, err := controller.ResolveRepositoryIdentity(cfg.RepoRoot)
	if err != nil {
		return controller.Admission{}, err
	}
	workspace, err := controller.ResolveWorkspaceIdentity(cfg.RepoRoot, identity)
	if err != nil {
		return controller.Admission{}, err
	}
	snapshot, err := controller.CaptureWorkspaceSnapshot(cfg.RepoRoot)
	if err != nil {
		return controller.Admission{}, err
	}
	return store.AdmitMutationOrFailClosed(authority, workspace, snapshot)
}

func executeControllerSemanticObserveTerminal(
	store *controller.Store,
	command controllerSemanticCommand,
) (controllerSemanticOutput, error) {
	if command.Observation == nil || command.AttemptID == "" || command.ProjectSnapshotID == "" {
		return controllerSemanticOutput{}, fmt.Errorf("terminal finding requires attempt, project snapshot, and observation")
	}
	finding, err := store.ObserveTerminalFinding(command.AttemptID, command.ProjectSnapshotID, *command.Observation)
	if err != nil {
		return controllerSemanticOutput{}, err
	}
	return controllerSemanticOutput{Action: command.Action, Finding: &finding}, nil
}

func executeControllerSemanticResolve(
	store *controller.Store,
	command controllerSemanticCommand,
) (controllerSemanticOutput, error) {
	if command.FindingID == "" || command.Decision == nil {
		return controllerSemanticOutput{}, fmt.Errorf("finding resolution requires finding identity and decision")
	}
	result, err := store.ResolveFinding(command.FindingID, *command.Decision)
	if err != nil {
		return controllerSemanticOutput{}, err
	}
	return controllerSemanticOutput{Action: command.Action, Result: &result}, nil
}

func executeControllerSemanticSchedule(
	store *controller.Store,
	command controllerSemanticCommand,
) (controllerSemanticOutput, error) {
	if command.EpisodeID == "" || command.EpisodeRevision == 0 {
		return controllerSemanticOutput{}, fmt.Errorf("episode scheduling requires episode identity and revision")
	}
	result, err := store.ScheduleEpisode(command.EpisodeID, command.EpisodeRevision)
	if err != nil {
		return controllerSemanticOutput{}, err
	}
	return controllerSemanticOutput{Action: command.Action, Schedule: &result}, nil
}
