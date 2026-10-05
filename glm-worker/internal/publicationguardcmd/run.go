package publicationguardcmd

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/publicationguard"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryharness"
)

type preToolUseInput struct {
	ToolName  string `json:"tool_name"`
	ToolInput struct {
		Command string `json:"command"`
	} `json:"tool_input"`
}

type preToolUseOutput struct {
	Decision string `json:"decision"`
	Code     string `json:"code"`
	Reason   string `json:"reason"`
}

func Run(args []string, stdin io.Reader, stdout io.Writer) error {
	if len(args) == 1 && args[0] == "probe" {
		_, err := fmt.Fprintln(stdout, publicationguard.Contract)
		return err
	}
	if len(args) == 1 && args[0] == "pre-tool-use" {
		return runPreToolUse(stdin, stdout)
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

func runPreToolUse(stdin io.Reader, stdout io.Writer) error {
	var input preToolUseInput
	if err := json.NewDecoder(stdin).Decode(&input); err != nil {
		return fmt.Errorf("decode publication PreToolUse input: %w", err)
	}
	if input.ToolName != "Bash" {
		return nil
	}
	code, reason := managedHookBypass(input.ToolInput.Command)
	if reason == "" {
		return nil
	}
	return json.NewEncoder(stdout).Encode(preToolUseOutput{Decision: "block", Code: code, Reason: reason})
}

func managedHookBypass(command string) (string, string) {
	if strings.Contains(command, "--no-verify") || managedGitCommitShortNoVerify(command) {
		return "managed_git_hook_bypass", "Git operation rejected: hook bypass may not bypass managed repository guards"
	}
	if strings.Contains(strings.ToLower(command), "core.hookspath") {
		return "managed_git_hook_bypass", "Git operation rejected: core.hooksPath may not bypass managed repository guards"
	}
	return "", ""
}

func managedGitCommitShortNoVerify(command string) bool {
	fields := strings.Fields(command)
	for index, field := range fields {
		if !shellCommandStart(fields, index) || filepath.Base(strings.Trim(field, `"'`)) != "git" {
			continue
		}
		commit := false
		for _, raw := range fields[index+1:] {
			token := strings.Trim(raw, `"'`)
			if shellCommandBoundary(token) {
				break
			}
			if token == "commit" {
				commit = true
				continue
			}
			if commit && token == "-n" {
				return true
			}
		}
	}
	return false
}

func shellCommandStart(fields []string, index int) bool {
	return index == 0 || shellCommandBoundary(strings.Trim(fields[index-1], `"'`))
}

func shellCommandBoundary(token string) bool {
	switch token {
	case ";", "&&", "||", "|":
		return true
	default:
		return false
	}
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
	return fmt.Errorf("usage: glm-publication-guard probe | pre-tool-use | ref-update --old <oid> --new <oid> --ref <ref> | push-update --remote-name <name> --local-ref <ref> --local-oid <oid> --remote-ref <ref> --remote-oid <oid>")
}
