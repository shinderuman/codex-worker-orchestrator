package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
)

type controllerEvidenceAction string

type controllerEvidenceCommand struct {
	Action          controllerEvidenceAction      `json:"action"`
	Ref             *controller.EvidenceObjectRef `json:"ref,omitempty"`
	LogicalIdentity string                        `json:"logical_identity,omitempty"`
	RootOIDs        []string                      `json:"root_oids,omitempty"`
}

type controllerEvidenceOutput struct {
	Action       controllerEvidenceAction             `json:"action"`
	CleanupProof *controller.CleanupDurabilityProof   `json:"cleanup_proof,omitempty"`
	Bundle       *controller.EvidenceBundleProjection `json:"bundle,omitempty"`
	ArchiveRef   *controller.EvidenceObjectRef        `json:"archive_ref,omitempty"`
	ArchiveRoots []controller.GitObjectArchiveRoot    `json:"archive_roots,omitempty"`
}

const (
	controllerEvidenceCleanup        controllerEvidenceAction = "cleanup-durability"
	controllerEvidenceCaptureArchive controllerEvidenceAction = "capture-git-archive"
	controllerEvidenceVerifyArchive  controllerEvidenceAction = "verify-git-archive"
	controllerEvidenceAttemptBundle  controllerEvidenceAction = "build-attempt-bundle"
	controllerEvidenceTaskBundle     controllerEvidenceAction = "build-task-bundle"
	controllerEvidenceEpisodeBundle  controllerEvidenceAction = "build-episode-bundle"
)

func runControllerEvidence(
	args []string,
	loadConfig func() (config.AppConfig, error),
	stdin io.Reader,
	stdout io.Writer,
) (bool, error) {
	if len(args) != 2 || args[0] != controllerAuthorityFlag || args[1] != "controller-evidence" {
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
	command, err := decodeControllerEvidenceCommand(stdin)
	if err != nil {
		return true, err
	}
	output, err := executeControllerEvidence(cfg, store, command)
	if err != nil {
		return true, err
	}
	return true, writeValidatedMachineJSON(stdout, output)
}

func decodeControllerEvidenceCommand(input io.Reader) (controllerEvidenceCommand, error) {
	decoder := json.NewDecoder(input)
	decoder.DisallowUnknownFields()
	var command controllerEvidenceCommand
	if err := decoder.Decode(&command); err != nil {
		return controllerEvidenceCommand{}, fmt.Errorf("decode controller evidence command: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return controllerEvidenceCommand{}, fmt.Errorf("controller evidence command contains trailing JSON")
	}
	if command.Action == "" {
		return controllerEvidenceCommand{}, fmt.Errorf("controller evidence action is required")
	}
	return command, nil
}

func executeControllerEvidence(
	cfg config.AppConfig,
	store *controller.Store,
	command controllerEvidenceCommand,
) (controllerEvidenceOutput, error) {
	switch command.Action {
	case controllerEvidenceCleanup:
		ref, err := requiredControllerEvidenceRef(command)
		if err != nil {
			return controllerEvidenceOutput{}, err
		}
		proof, err := store.ProveCleanupDurability(ref)
		if err != nil {
			return controllerEvidenceOutput{}, err
		}
		return controllerEvidenceOutput{Action: command.Action, CleanupProof: &proof}, nil
	case controllerEvidenceCaptureArchive:
		if command.LogicalIdentity == "" || len(command.RootOIDs) == 0 {
			return controllerEvidenceOutput{}, fmt.Errorf("Git object archive capture requires logical identity and roots")
		}
		ref, roots, err := store.CaptureGitObjectArchive(cfg.RepoRoot, command.LogicalIdentity, command.RootOIDs)
		if err != nil {
			return controllerEvidenceOutput{}, err
		}
		return controllerEvidenceOutput{Action: command.Action, ArchiveRef: &ref, ArchiveRoots: roots}, nil
	case controllerEvidenceVerifyArchive:
		ref, err := requiredControllerEvidenceRef(command)
		if err != nil {
			return controllerEvidenceOutput{}, err
		}
		roots, err := store.VerifyGitObjectArchive(ref)
		if err != nil {
			return controllerEvidenceOutput{}, err
		}
		return controllerEvidenceOutput{Action: command.Action, ArchiveRef: &ref, ArchiveRoots: roots}, nil
	case controllerEvidenceAttemptBundle:
		ref, err := requiredControllerEvidenceRef(command)
		if err != nil {
			return controllerEvidenceOutput{}, err
		}
		bundle, err := store.BuildAttemptEvidenceBundle(ref)
		if err != nil {
			return controllerEvidenceOutput{}, err
		}
		return controllerEvidenceOutput{Action: command.Action, Bundle: &bundle}, nil
	case controllerEvidenceTaskBundle:
		ref, err := requiredControllerEvidenceRef(command)
		if err != nil {
			return controllerEvidenceOutput{}, err
		}
		bundle, err := store.BuildTaskEvidenceBundle(ref)
		if err != nil {
			return controllerEvidenceOutput{}, err
		}
		return controllerEvidenceOutput{Action: command.Action, Bundle: &bundle}, nil
	case controllerEvidenceEpisodeBundle:
		ref, err := requiredControllerEvidenceRef(command)
		if err != nil {
			return controllerEvidenceOutput{}, err
		}
		bundle, err := store.BuildEpisodeEvidenceBundle(ref)
		if err != nil {
			return controllerEvidenceOutput{}, err
		}
		return controllerEvidenceOutput{Action: command.Action, Bundle: &bundle}, nil
	default:
		return controllerEvidenceOutput{}, fmt.Errorf("unsupported controller evidence action %q", command.Action)
	}
}

func requiredControllerEvidenceRef(command controllerEvidenceCommand) (controller.EvidenceObjectRef, error) {
	if command.Ref == nil {
		return controller.EvidenceObjectRef{}, fmt.Errorf("controller evidence action %q requires evidence ref", command.Action)
	}
	return *command.Ref, nil
}
