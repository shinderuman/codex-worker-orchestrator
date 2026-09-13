package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentfix"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type CommandMode int

type Command struct {
	Mode    CommandMode
	Payload string

	WatchVerbose bool

	StdinBytes int64

	SHA256 string

	Origin              string
	Cause               string
	AcceptedScope       string
	ExecutionMilestones bool
	Role                string
	ArtifactRoot        string
	Verify              VerifyArgs
	Coalesce            CoalesceArgs
	CodexWake           CodexWakeArgs
	Query               TelemetryQueryArgs
	SearchScopes        []string
	SearchBudgetBytes   int
	EvidenceManifest    string
}

type VerifyArgs struct {
	Key      string
	RFC3339  string
	ThreadID string
}

type CoalesceArgs struct {
	ParentThreadID  string
	ResumeAtRFC3339 string
}

type TelemetryQueryArgs struct {
	Scope   string
	Filter  state.TelemetryQueryFilter
	Compact bool
}

type UsageError struct {
	Message string
}

type NotFoundError struct {
	Message string
}

type StdinPayloadError struct {
	Message string
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
	ModeIsolate
	ModePark
	ModeUnpark
	ModeStatus
	ModeHandoff
	ModeWatch
	ModeTimeline
	ModeConvergence
	ModeStats
	ModeReset
	ModeVerifyAutoResume
	ModeVerifyCodexWake
	ModeCheckWakeCoalesce
	ModeEvalAB
	ModeCallOutliers
	ModeCodexLimit
	ModeInstallSmoke
	ModeQualityGate
	ModeModelRouting
	ModeTestImpact
	ModeBundle
	ModeParentUsage
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
)

const fixOriginUsage = "[--origin codex-review|glm-reviewer|user-amendment|external-review|metadata-repair] [--cause parent-orchestration|requirement-preservation|worker|reviewer|sol-gate|production-wiring|test-scenario|cross-cutting-invariant|unknown] [--accepted-scope current-diff]"

const approveSurfaceUsage = "usage: glm-worker --approve-surface current-diff"

const acceptedFixScopeCurrentDiffCLI = "current-diff"

const installSmokeUsage = "[--role worker|reviewer|fix|parent]"

const repoSearchUsage = "usage: glm-worker --repo-search <question> --scope <path|symbol:<identifier>> [--scope ...] --budget <bytes>"

const evidenceUsage = "usage: glm-worker --evidence <manifest.json>"

const repoSearchMaxBudgetBytes = 64 * 1024

const telemetryQueryUsage = "[current|history] [--task <task-id>] [--since <rfc3339>] [--until <rfc3339>] [--compact]"

const verifyCodexWakeUsage = "usage: glm-worker --verify-codex-wake <wake-task-thread-id> <wake-at-rfc3339>"

var commandParsers = map[string]commandParser{
	"--decision": func([]string) (Command, error) {
		return Command{}, usageError("usage: glm-worker --decision-stdin <payload-bytes> [--sha256 <hex>] | --fix-stdin <payload-bytes> [--sha256 <hex>] %s", fixOriginUsage)
	},
	"--fix": func([]string) (Command, error) {
		return Command{}, usageError("usage: glm-worker --decision-stdin <payload-bytes> [--sha256 <hex>] | --fix-stdin <payload-bytes> [--sha256 <hex>] %s", fixOriginUsage)
	},
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
			return Command{}, usageError("%s", approveSurfaceUsage)
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
	"--isolate": func(args []string) (Command, error) {
		return singleArgCommand(args, ModeIsolate, "usage: glm-worker --isolate")
	},
	"--park": func(args []string) (Command, error) {
		return singleArgCommand(args, ModePark, "usage: glm-worker --park")
	},
	"--unpark": func(args []string) (Command, error) {
		return singleArgCommand(args, ModeUnpark, "usage: glm-worker --unpark")
	},
	"--status": func(args []string) (Command, error) {
		return singleArgCommand(args, ModeStatus, "usage: glm-worker --status")
	},
	"--handoff": parentHandoffCommand,
	"--watch":   watchCommand,
	"--timeline": func(args []string) (Command, error) {
		return optionalPayloadCommand(args, ModeTimeline, "usage: glm-worker --timeline [task-id]")
	},
	"--convergence": func(args []string) (Command, error) {
		return optionalPayloadCommand(args, ModeConvergence, "usage: glm-worker --convergence [task-id]")
	},
	"--stats": func(args []string) (Command, error) {
		return telemetryQueryCommand(args, ModeStats, "--stats")
	},
	"--reset": func(args []string) (Command, error) {
		return singleArgCommand(args, ModeReset, "usage: glm-worker --reset")
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
	"--verify-auto-resume":        verifyAutoResumeCommand,
	"--verify-codex-wake":         verifyCodexWakeCommand,
	"--check-wake-coalesce":       checkWakeCoalesceCommand,
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
	"--repo-search": repoSearchCommand,
	"--evidence":    evidenceCommand,
	"--repo-search-eval": func(args []string) (Command, error) {
		return singleArgCommand(args, ModeRepoSearchEval, "usage: glm-worker --repo-search-eval")
	},
	"--install-smoke": installSmokeCommand,
	"--quality-gate":  qualityGateRecoveryCommand,
	"--packet-check":  packetCheckCommand,
	"--project-state": func(args []string) (Command, error) {
		return singleArgCommand(args, ModeProjectState, "usage: glm-worker --project-state")
	},
	"bundle": func(args []string) (Command, error) {
		return optionalPayloadCommand(args, ModeBundle, "usage: glm-worker bundle [task-id]")
	},
	"--parent-usage": func(args []string) (Command, error) {
		return optionalPayloadCommand(args, ModeParentUsage, "usage: glm-worker --parent-usage [task-id]")
	},
	"--review-gap": func(args []string) (Command, error) {
		return optionalPayloadCommand(args, ModeReviewGap, "usage: glm-worker --review-gap [task-id]")
	},
}

func (e *UsageError) Error() string {
	return e.Message
}

func (e *NotFoundError) Error() string {
	return e.Message
}

func usageError(format string, args ...any) *UsageError {
	return &UsageError{Message: fmt.Sprintf(format, args...)}
}

func ParseCommand(args []string) (Command, error) {
	if len(args) == 0 {
		return Command{}, usageError("usage: glm-worker <instruction> | <command>; run glm-worker --help for command list")
	}
	if parser, ok := commandParsers[args[0]]; ok {
		return parser(args)
	}
	return Command{Mode: ModeNewTask, Payload: strings.Join(args, " ")}, nil
}

func singleArgCommand(args []string, mode CommandMode, usage string) (Command, error) {
	if len(args) != 1 {
		return Command{}, usageError("%s", usage)
	}
	return Command{Mode: mode}, nil
}

func optionalPayloadCommand(args []string, mode CommandMode, usage string) (Command, error) {
	if len(args) > 2 {
		return Command{}, usageError("%s", usage)
	}
	command := Command{Mode: mode}
	if len(args) == 2 {
		command.Payload = args[1]
	}
	return command, nil
}

func requiredPayloadCommand(args []string, mode CommandMode, usage string) (Command, error) {
	if len(args) != 2 {
		return Command{}, usageError("%s", usage)
	}
	return Command{Mode: mode, Payload: args[1]}, nil
}

func evidenceCommand(args []string) (Command, error) {
	if len(args) != 2 {
		return Command{}, usageError("%s", evidenceUsage)
	}
	return Command{Mode: ModeEvidence, EvidenceManifest: args[1]}, nil
}

func parentHandoffCommand(args []string) (Command, error) {
	if len(args) == 1 {
		return Command{Mode: ModeHandoff}, nil
	}
	if len(args) == 2 && args[1] == "recovery" {
		return Command{Mode: ModeHandoff, Payload: "recovery"}, nil
	}
	return Command{}, usageError("usage: glm-worker --handoff [recovery]")
}

func watchCommand(args []string) (Command, error) {
	if len(args) == 1 {
		return Command{Mode: ModeWatch}, nil
	}
	if len(args) == 2 && args[1] == "--verbose" {
		return Command{Mode: ModeWatch, WatchVerbose: true}, nil
	}
	return Command{}, usageError("usage: glm-worker --watch [--verbose]")
}

func verifyAutoResumeCommand(args []string) (Command, error) {
	if len(args) != 3 {
		return Command{}, usageError("usage: glm-worker --verify-auto-resume <automation-key> <auto-resume-at-rfc3339>")
	}
	return Command{
		Mode: ModeVerifyAutoResume,
		Verify: VerifyArgs{
			Key:     args[1],
			RFC3339: args[2],
		},
	}, nil
}

func verifyCodexWakeCommand(args []string) (Command, error) {
	if len(args) != 3 || !state.ValidUUIDFormat(args[1]) {
		return Command{}, usageError("%s", verifyCodexWakeUsage)
	}
	return Command{
		Mode: ModeVerifyCodexWake,
		Verify: VerifyArgs{
			ThreadID: args[1],
			RFC3339:  args[2],
		},
	}, nil
}

func checkWakeCoalesceCommand(args []string) (Command, error) {
	if len(args) != 2 {
		return Command{}, usageError("usage: glm-worker --check-wake-coalesce <auto-resume-at-rfc3339>")
	}
	return Command{
		Mode: ModeCheckWakeCoalesce,
		Coalesce: CoalesceArgs{
			ResumeAtRFC3339: args[1],
		},
	}, nil
}

func installSmokeCommand(args []string) (Command, error) {
	if len(args) == 1 {
		return Command{Mode: ModeInstallSmoke}, nil
	}
	if len(args) == 3 && args[1] == "--role" && validInstallSmokeRoles[args[2]] {
		return Command{Mode: ModeInstallSmoke, Role: args[2]}, nil
	}
	return Command{}, usageError("usage: glm-worker --install-smoke %s", installSmokeUsage)
}

func repoSearchCommand(args []string) (Command, error) {
	if len(args) < 2 || args[1] == "" || len(args[2:])%2 != 0 {
		return Command{}, usageError("%s", repoSearchUsage)
	}
	command := Command{Mode: ModeRepoSearch, Payload: args[1]}
	seenBudget := false
	for index := 2; index < len(args); index += 2 {
		budgetSeen, err := applyRepoSearchOption(&command, args[index], args[index+1])
		if err != nil {
			return Command{}, err
		}
		seenBudget = seenBudget || budgetSeen
	}
	if len(command.SearchScopes) == 0 || !seenBudget {
		return Command{}, usageError("%s", repoSearchUsage)
	}
	return command, nil
}

func applyRepoSearchOption(command *Command, name string, value string) (bool, error) {
	switch name {
	case "--scope":
		if value == "" {
			return false, usageError("%s", repoSearchUsage)
		}
		command.SearchScopes = append(command.SearchScopes, value)
		return false, nil
	case "--budget":
		budget, err := strconv.Atoi(value)
		if err != nil || budget <= 0 || budget > repoSearchMaxBudgetBytes {
			return false, usageError("%s", repoSearchUsage)
		}
		if command.SearchBudgetBytes != 0 {
			return false, usageError("%s", repoSearchUsage)
		}
		command.SearchBudgetBytes = budget
		return true, nil
	default:
		return false, usageError("%s", repoSearchUsage)
	}
}

func stdinPayloadCommand(mode CommandMode, args []string, usage string, allowFixOptions bool) (Command, error) {
	if len(args) < 2 {
		return Command{}, usageError("%s", usage)
	}
	payloadBytes, err := strconv.ParseInt(args[1], 10, 64)
	if err != nil || payloadBytes <= 0 {
		return Command{}, usageError("%s", usage)
	}

	semantic := parentfix.Options{}
	options := args[2:]
	if allowFixOptions {
		semantic, options, err = parentfix.Extract(options)
		if err != nil {
			return Command{}, usageError("%s", usage)
		}
	}
	if len(options)%2 != 0 {
		return Command{}, usageError("%s", usage)
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
		return usageError("%s", usage)
	}
	digest, err := parsePayloadSHA256(value)
	if err != nil {
		return usageError("%s", usage)
	}
	command.SHA256 = digest
	*seenSHA256 = true
	return nil
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

func (e *StdinPayloadError) Error() string {
	return e.Message
}

func readStdinPayload(in io.Reader, want int64, expectedSHA string) (string, error) {
	var buf bytes.Buffer
	written, err := io.CopyN(&buf, in, want)
	if err != nil {
		return "", &StdinPayloadError{Message: fmt.Sprintf("stdin payload read failed after %d of %d bytes: %v", written, want, err)}
	}

	payload := buf.Bytes()
	if expectedSHA != "" {
		sum := sha256.Sum256(payload)
		actual := hex.EncodeToString(sum[:])
		if !strings.EqualFold(actual, expectedSHA) {
			return "", &StdinPayloadError{Message: fmt.Sprintf("stdin payload sha256 mismatch: expected %s, got %s", expectedSHA, actual)}
		}
	}
	return string(payload), nil
}
