package publicationguardcmd

import (
	"fmt"
	"io"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
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
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	decision, err := repositoryharness.Evaluate(cfg.RepoRoot)
	if err != nil {
		return fmt.Errorf("inspect repository publication guard activation: %w", err)
	}
	if !decision.Active {
		return nil
	}
	exists, err := controller.Exists(cfg)
	if err != nil {
		return err
	}
	if !exists {
		if args[0] == "ref-update" {
			_, err := parseRefUpdate(args[1:])
			return err
		}
		return fmt.Errorf("publication push rejected: controller authority is unavailable")
	}
	store, err := controller.Open(cfg)
	if err != nil {
		return err
	}
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
