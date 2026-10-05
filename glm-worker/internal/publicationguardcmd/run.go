package publicationguardcmd

import (
	"fmt"
	"io"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/publicationguard"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryharness"
)

const Contract = "controller-publication-guard-v1"

func Run(args []string, stdout io.Writer) error {
	if len(args) == 1 && args[0] == "probe" {
		_, err := fmt.Fprintln(stdout, Contract)
		return err
	}
	if len(args) == 0 {
		return usageError()
	}
	cfg, active, err := activeGuardConfig()
	if err != nil {
		return err
	}
	if !active {
		return nil
	}
	if err := requireGuardSetup(cfg.RepoRoot); err != nil {
		return err
	}
	return runActiveGuard(args, cfg)
}

func activeGuardConfig() (config.AppConfig, bool, error) {
	cfg, err := config.Load()
	if err != nil {
		return config.AppConfig{}, false, err
	}
	decision, err := repositoryharness.Evaluate(cfg.RepoRoot)
	if err != nil {
		return config.AppConfig{}, false, fmt.Errorf("inspect repository publication guard activation: %w", err)
	}
	return cfg, decision.Active, nil
}

func requireGuardSetup(repoRoot string) error {
	report, err := publicationguard.InspectPublicationGuardSetup(repoRoot)
	if err != nil {
		return err
	}
	if len(report.Defects) == 0 {
		return nil
	}
	defect := report.Defects[0]
	return fmt.Errorf("publication guard setup invalid: %s %s at %s", defect.Hook, defect.Defect, defect.Path)
}

func runActiveGuard(args []string, cfg config.AppConfig) error {
	exists, err := controller.Exists(cfg)
	if err != nil {
		return err
	}
	if !exists {
		return runWithoutController(args)
	}
	store, err := controller.Open(cfg)
	if err != nil {
		return err
	}
	return runWithController(args, store)
}

func runWithoutController(args []string) error {
	if args[0] != "ref-update" {
		return fmt.Errorf("publication push rejected: controller authority is unavailable")
	}
	_, err := parseRefUpdate(args[1:])
	return err
}

func runWithController(args []string, store *controller.Store) error {
	switch args[0] {
	case "ref-update":
		input, err := parseRefUpdate(args[1:])
		if err != nil {
			return err
		}
		return store.GuardPublicationRefUpdate(input)
	case "push-update":
		input, err := parsePushUpdate(args[1:])
		if err != nil {
			return err
		}
		return store.GuardPublicationPush(input)
	default:
		return usageError()
	}
}

func parseRefUpdate(args []string) (controller.PublicationRefGuardInput, error) {
	if len(args) != 6 || args[0] != "--old" || args[2] != "--new" || args[4] != "--ref" {
		return controller.PublicationRefGuardInput{}, usageError()
	}
	return controller.PublicationRefGuardInput{OldOID: args[1], NewOID: args[3], Ref: args[5]}, nil
}

func parsePushUpdate(args []string) (controller.PublicationPushGuardInput, error) {
	if len(args) != 10 || args[0] != "--remote-name" || args[2] != "--local-ref" || args[4] != "--local-oid" || args[6] != "--remote-ref" || args[8] != "--remote-oid" {
		return controller.PublicationPushGuardInput{}, usageError()
	}
	return controller.PublicationPushGuardInput{
		RemoteName: args[1],
		LocalRef:   args[3],
		LocalOID:   args[5],
		RemoteRef:  args[7],
		RemoteOID:  args[9],
	}, nil
}

func usageError() error {
	return fmt.Errorf("usage: glm-publication-guard probe | ref-update --old <oid> --new <oid> --ref <ref> | push-update --remote-name <name> --local-ref <ref> --local-oid <oid> --remote-ref <ref> --remote-oid <oid>")
}
