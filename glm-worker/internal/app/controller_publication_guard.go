package app

import (
	"fmt"
	"io"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryharness"
)

const (
	controllerPublicationGuardSurface = "controller-publication-guard"
	controllerPublicationGuardVersion = 1
)

type controllerPublicationGuardOutput struct {
	Status  string `json:"status"`
	Surface string `json:"surface"`
	Version int    `json:"version"`
}

func runControllerPublicationGuard(
	args []string,
	loadConfig func() (config.AppConfig, error),
	stdout io.Writer,
) (bool, error) {
	if len(args) < 3 || args[0] != controllerAuthorityFlag || args[1] != controllerPublicationGuardSurface {
		return false, nil
	}
	if len(args) == 3 && args[2] == "probe" {
		return true, writeControllerPublicationGuardAllowed(stdout)
	}
	cfg, err := loadConfig()
	if err != nil {
		return true, err
	}
	decision, err := repositoryharness.Evaluate(cfg.RepoRoot)
	if err != nil {
		return true, fmt.Errorf("inspect repository publication guard activation: %w", err)
	}
	if !decision.Active {
		return true, writeControllerPublicationGuardAllowed(stdout)
	}
	exists, err := controller.Exists(cfg)
	if err != nil {
		return true, err
	}
	if !exists {
		return true, fmt.Errorf("active repository publication guard requires existing controller authority")
	}
	store, err := controller.Open(cfg)
	if err != nil {
		return true, err
	}
	switch args[2] {
	case "ref-update":
		input, err := parseControllerPublicationRefGuard(args[3:])
		if err != nil {
			return true, err
		}
		if err := store.GuardPublicationRefUpdate(input); err != nil {
			return true, err
		}
	case "push-update":
		input, err := parseControllerPublicationPushGuard(args[3:])
		if err != nil {
			return true, err
		}
		if err := store.GuardPublicationPush(input); err != nil {
			return true, err
		}
	default:
		return true, fmt.Errorf("unsupported controller publication guard action %q", args[2])
	}
	return true, writeControllerPublicationGuardAllowed(stdout)
}

func writeControllerPublicationGuardAllowed(stdout io.Writer) error {
	return writeValidatedMachineJSON(stdout, controllerPublicationGuardOutput{
		Status:  "allowed",
		Surface: controllerPublicationGuardSurface,
		Version: controllerPublicationGuardVersion,
	})
}

func parseControllerPublicationRefGuard(args []string) (controller.PublicationRefGuardInput, error) {
	if len(args) != 6 || args[0] != "--old" || args[2] != "--new" || args[4] != "--ref" {
		return controller.PublicationRefGuardInput{}, fmt.Errorf("usage: glm-worker --authority %s ref-update --old <oid> --new <oid> --ref <ref>", controllerPublicationGuardSurface)
	}
	return controller.PublicationRefGuardInput{OldOID: args[1], NewOID: args[3], Ref: args[5]}, nil
}

func parseControllerPublicationPushGuard(args []string) (controller.PublicationPushGuardInput, error) {
	if len(args) != 10 || args[0] != "--remote-name" || args[2] != "--local-ref" || args[4] != "--local-oid" || args[6] != "--remote-ref" || args[8] != "--remote-oid" {
		return controller.PublicationPushGuardInput{}, fmt.Errorf("usage: glm-worker --authority %s push-update --remote-name <name> --local-ref <ref> --local-oid <oid> --remote-ref <ref> --remote-oid <oid>", controllerPublicationGuardSurface)
	}
	return controller.PublicationPushGuardInput{
		RemoteName: args[1],
		LocalRef:   args[3],
		LocalOID:   args[5],
		RemoteRef:  args[7],
		RemoteOID:  args[9],
	}, nil
}
