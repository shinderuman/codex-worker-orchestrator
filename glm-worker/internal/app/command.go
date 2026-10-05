package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/machinecli"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentfix"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/report"
)

type CommandMode int

type Command struct {
	Mode    CommandMode
	Payload string

	StdinBytes int64

	SHA256 string

	Origin              string
	Cause               string
	AcceptedScope       string
	ExecutionMilestones bool
	Role                string
	ArtifactRoot        string
	Verify              VerifyArgs
	CodexWake           CodexWakeArgs
	Query               report.Query
	SearchScopes        []string
	SearchBudgetBytes   int
	EvidenceManifest    string
	ReferencePath       string
}

type VerifyArgs struct {
	RFC3339  string
	ThreadID string
}

type commandParser func([]string) (Command, error)

const (
	ModeNewTask CommandMode = iota
	ModeDecision
	ModeFix
	ModeApproveSurface
	ModeAccept
	ModeResume
	ModeStop
	ModeStatus
	ModeHandoff
	ModeTimeline
	ModeConvergence
	ModeStats
	ModeVerifyCodexWake
	ModeEvalAB
	ModeCallOutliers
	ModeCodexLimit
	ModeInstallSmoke
	ModeQualityGate
	ModeModelRouting
	ModeTestImpact
	ModeReviewGap
	ModeRepoSearch
	ModeRepoSearchEval
	ModeExecutionMilestonesRevise
	ModePacketCheck
	ModeProjectState
	ModeEvidence
	modeRotateInstructionBaseline
	modeRecoverParentAction
	modeRecoverQualitySurface
	ModeCodexWakePlan
	ModeCodexWakeResponse
	ModeShadowEval
	ModeFailurePathAdvisory
)

const fixOriginUsage = "[--origin codex-review|glm-reviewer|user-amendment|external-review|metadata-repair] [--cause parent-orchestration|requirement-preservation|worker|reviewer|sol-gate|production-wiring|test-scenario|cross-cutting-invariant|unknown] [--accepted-scope current-diff]"

const approveSurfaceUsage = "usage: glm-worker --approve-surface current-diff"

const acceptedFixScopeCurrentDiffCLI = "current-diff"

var commandParsers = map[string]commandParser{
	"--decision-stdin": func(args []string) (Command, error) {
		return stdinPayloadCommand(ModeDecision, args, "usage: glm-worker --decision-stdin <payload-bytes> [--sha256 <hex>]", false)
	},
	"--fix-stdin": func(args []string) (Command, error) {
		return stdinPayloadCommand(ModeFix, args, fmt.Sprintf("usage: glm-worker --fix-stdin <payload-bytes> [--sha256 <hex>] %s", fixOriginUsage), true)
	},
	"--execution-milestones-stdin":        executionMilestoneTaskCommand,
	"--execution-milestones-revise-stdin": executionMilestoneRevisionCommand,
	"--approve-surface": func(args []string) (Command, error) {
		if len(args) != 2 || args[1] != acceptedFixScopeCurrentDiffCLI {
			return Command{}, machinecli.UsageErrorf("%s", approveSurfaceUsage)
		}
		return Command{Mode: ModeApproveSurface, AcceptedScope: acceptedFixScopeCurrentDiffCLI}, nil
	},
	"--accept": func(args []string) (Command, error) {
		return singleArgCommand(args, ModeAccept, "usage: glm-worker --accept")
	},
	"--resume": func(args []string) (Command, error) {
		return singleArgCommand(args, ModeResume, "usage: glm-worker --resume")
	},
	"--stop": func(args []string) (Command, error) {
		return singleArgCommand(args, ModeStop, "usage: glm-worker --stop")
	},
	"--status": func(args []string) (Command, error) {
		return singleArgCommand(args, ModeStatus, "usage: glm-worker --status")
	},
	"--handoff": parentHandoffCommand,
	"--timeline": func(args []string) (Command, error) {
		return optionalPayloadCommand(args, ModeTimeline, "usage: glm-worker --timeline [task-id]")
	},
	"--convergence": func(args []string) (Command, error) {
		return optionalPayloadCommand(args, ModeConvergence, "usage: glm-worker --convergence [task-id]")
	},
	"--stats": func(args []string) (Command, error) {
		return telemetryQueryCommand(args, ModeStats, "--stats")
	},
	"--rotate-instruction-baseline": func(args []string) (Command, error) {
		return singleArgCommand(args, modeRotateInstructionBaseline, "usage: glm-worker --rotate-instruction-baseline")
	},
	"--recover-parent-action": func(args []string) (Command, error) {
		return singleArgCommand(args, modeRecoverParentAction, "usage: glm-worker --recover-parent-action")
	},
	"--recover-quality-surface": func(args []string) (Command, error) {
		return requiredPayloadCommand(args, modeRecoverQualitySurface, "usage: glm-worker --recover-quality-surface <task-id>")
	},
	"--verify-codex-wake":         verifyCodexWakeCommand,
	"--codex-wake-plan":           codexWakePlanCommand,
	"--codex-wake-response-stdin": codexWakeResponseCommand,
	"--eval-ab": func(args []string) (Command, error) {
		return requiredPayloadCommand(args, ModeEvalAB, "usage: glm-worker --eval-ab <run-dir>")
	},
	"--call-outliers": func(args []string) (Command, error) {
		return telemetryQueryCommand(args, ModeCallOutliers, "--call-outliers")
	},
	"--model-routing": func(args []string) (Command, error) {
		return singleArgCommand(args, ModeModelRouting, "usage: glm-worker --model-routing")
	},
	"--test-impact": func(args []string) (Command, error) {
		return singleArgCommand(args, ModeTestImpact, "usage: glm-worker --test-impact")
	},
	"--codex-limit": func(args []string) (Command, error) {
		return singleArgCommand(args, ModeCodexLimit, "usage: glm-worker --codex-limit")
	},
	"--repo-search":           repoSearchCommand,
	"--evidence":              evidenceCommand,
	"--shadow-eval":           shadowEvalCommand,
	"--failure-path-advisory": failurePathAdvisoryCommand,
	"--repo-search-eval": func(args []string) (Command, error) {
		return singleArgCommand(args, ModeRepoSearchEval, "usage: glm-worker --repo-search-eval")
	},
	"--install-smoke": installSmokeCommand,
	"--quality-gate":  qualityGateRecoveryCommand,
	"--packet-check":  packetCheckCommand,
	"--project-state": func(args []string) (Command, error) {
		return singleArgCommand(args, ModeProjectState, "usage: glm-worker --project-state")
	},
	"--review-gap": func(args []string) (Command, error) {
		return optionalPayloadCommand(args, ModeReviewGap, "usage: glm-worker --review-gap [task-id]")
	},
}

func ParseCommand(args []string) (Command, error) {
	if len(args) == 0 {
		return Command{}, machinecli.UsageErrorf("usage: glm-worker <instruction> | <command>; run glm-worker --help for command list")
	}
	if parser, ok := commandParsers[args[0]]; ok {
		return parser(args)
	}
	if strings.HasPrefix(args[0], "--") {
		return Command{}, machinecli.UsageErrorf("unknown command %q; run glm-worker --help for command list", args[0])
	}
	return Command{Mode: ModeNewTask, Payload: strings.Join(args, " ")}, nil
}

func singleArgCommand(args []string, mode CommandMode, usage string) (Command, error) {
	if len(args) != 1 {
		return Command{}, machinecli.UsageErrorf("%s", usage)
	}
	return Command{Mode: mode}, nil
}

func optionalPayloadCommand(args []string, mode CommandMode, usage string) (Command, error) {
	if len(args) > 2 {
		return Command{}, machinecli.UsageErrorf("%s", usage)
	}
	command := Command{Mode: mode}
	if len(args) == 2 {
		command.Payload = args[1]
	}
	return command, nil
}

func requiredPayloadCommand(args []string, mode CommandMode, usage string) (Command, error) {
	if len(args) != 2 {
		return Command{}, machinecli.UsageErrorf("%s", usage)
	}
	return Command{Mode: mode, Payload: args[1]}, nil
}

func stdinPayloadCommand(mode CommandMode, args []string, usage string, allowFixOptions bool) (Command, error) {
	if len(args) < 2 {
		return Command{}, machinecli.UsageErrorf("%s", usage)
	}
	payloadBytes, err := strconv.ParseInt(args[1], 10, 64)
	if err != nil || payloadBytes <= 0 {
		return Command{}, machinecli.UsageErrorf("%s", usage)
	}

	semantic := parentfix.Options{}
	options := args[2:]
	if allowFixOptions {
		semantic, options, err = parentfix.Extract(options)
		if err != nil {
			return Command{}, machinecli.UsageErrorf("%s", usage)
		}
	}
	if len(options)%2 != 0 {
		return Command{}, machinecli.UsageErrorf("%s", usage)
	}
	command := Command{
		Mode:          mode,
		StdinBytes:    payloadBytes,
		Origin:        semantic.Origin,
		Cause:         semantic.Cause,
		AcceptedScope: semantic.AcceptedScope,
	}
	seenSHA256 := false
	for index := 0; index < len(options); index += 2 {
		if err := applyStdinPayloadOption(&command, options[index], options[index+1], usage, &seenSHA256); err != nil {
			return Command{}, err
		}
	}
	return command, nil
}

func applyStdinPayloadOption(command *Command, name, value, usage string, seenSHA256 *bool) error {
	if name != "--sha256" || *seenSHA256 {
		return machinecli.UsageErrorf("%s", usage)
	}
	digest, err := parsePayloadSHA256(value)
	if err != nil {
		return machinecli.UsageErrorf("%s", usage)
	}
	command.SHA256 = digest
	*seenSHA256 = true
	return nil
}

func transactionResponseCommand(args []string, mode CommandMode, usage string, bindToken func(*Command, string)) (Command, error) {
	if len(args) != 3 && len(args) != 5 {
		return Command{}, machinecli.UsageErrorf("%s", usage)
	}
	payloadBytes, err := strconv.ParseInt(args[1], 10, 64)
	if err != nil || payloadBytes <= 0 || args[2] == "" {
		return Command{}, machinecli.UsageErrorf("%s", usage)
	}
	command := Command{Mode: mode, StdinBytes: payloadBytes}
	bindToken(&command, args[2])
	if len(args) == 5 {
		seenSHA256 := false
		if err := applyStdinPayloadOption(&command, args[3], args[4], usage, &seenSHA256); err != nil {
			return Command{}, err
		}
	}
	return command, nil
}

func parsePayloadSHA256(value string) (string, error) {
	if len(value) != 64 {
		return "", fmt.Errorf("sha256 must be 64 hex characters")
	}
	for _, c := range value {
		if !strings.ContainsRune("0123456789abcdefABCDEF", c) {
			return "", fmt.Errorf("sha256 must be 64 hex characters")
		}
	}
	return strings.ToLower(value), nil
}

func readStdinPayload(in io.Reader, want int64, expectedSHA string) (string, error) {
	var buf bytes.Buffer
	written, err := io.CopyN(&buf, in, want)
	if err != nil {
		return "", &machinecli.StdinPayloadError{Message: fmt.Sprintf("stdin payload read failed after %d of %d bytes: %v", written, want, err)}
	}

	payload := buf.Bytes()
	if expectedSHA != "" {
		sum := sha256.Sum256(payload)
		actual := hex.EncodeToString(sum[:])
		if !strings.EqualFold(actual, expectedSHA) {
			return "", &machinecli.StdinPayloadError{Message: fmt.Sprintf("stdin payload sha256 mismatch: expected %s, got %s", expectedSHA, actual)}
		}
	}
	return string(payload), nil
}
