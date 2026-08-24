// Package appはCLI引数解析・実行調停・プロセス間ロック・コマンド出力を担う。
package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/workflow"
)

type CommandMode int

const (
	ModeNewTask CommandMode = iota
	ModeDecision
	ModeFix
	ModeAccept
	ModeResume
	ModeStatus
	ModeWatch
	ModeTimeline
	ModeConvergence
	ModeStats
	ModeReset
	ModeVerifyAutoResume
	ModeEvalAB
)

type Command struct {
	Mode    CommandMode
	Payload string
	// WatchVerboseは--watch --verboseの明示的詳細表示指定。--watch単体の表示は不変。
	WatchVerbose bool
	// StdinBytesはdecision/fix payloadをstdinから読み取るbyte数。
	StdinBytes int64
	// SHA256はstdin payloadの送信元が計算した期待値。空なら照合しない。
	SHA256 string
	// Originは--fix/--fix-stdinの--origin宣言値。fix originの有限集合のどれかで、
	// 空は未宣言(unknown origin)を意味する。
	Origin string
	Verify VerifyArgs
}

type VerifyArgs struct {
	Key      string
	RFC3339  string
	ThreadID string
}

// fixOriginUsageは--originが受け付けるfix origin有限集合の表示。
const fixOriginUsage = "[--origin codex-review|glm-reviewer|user-amendment|external-review|metadata-repair]"

// UsageErrorは引数・task ID等の呼出形式の不正。process errorのkind usageへ対応する。
type UsageError struct {
	Message string
}

func (e *UsageError) Error() string {
	return e.Message
}

// NotFoundErrorは明示指定の対象(log・run dir等)が存在しない失敗。process errorのkind
// not_foundへ対応する。
type NotFoundError struct {
	Message string
}

func (e *NotFoundError) Error() string {
	return e.Message
}

func usageError(format string, args ...any) *UsageError {
	return &UsageError{Message: fmt.Sprintf(format, args...)}
}

func ParseCommand(args []string) (Command, error) {
	if len(args) == 0 {
		return Command{}, usageError("usage: glm-worker <instruction> | --decision-stdin <payload-bytes> [--sha256 <hex>] | --fix-stdin <payload-bytes> [--sha256 <hex>] %s | --accept | --resume | --status | --watch [--verbose] | --timeline [task-id] | --convergence [task-id] | --stats | --reset | --eval-ab <run-dir>", fixOriginUsage)
	}

	switch args[0] {
	// 廃止したargv埋込みmodeを新規task本文へ誤routingさせないため、専用のusage errorで
	// fail closedする。
	case "--decision", "--fix":
		return Command{}, usageError("usage: glm-worker --decision-stdin <payload-bytes> [--sha256 <hex>] | --fix-stdin <payload-bytes> [--sha256 <hex>] %s", fixOriginUsage)
	case "--decision-stdin":
		return stdinPayloadCommand(ModeDecision, args, "usage: glm-worker --decision-stdin <payload-bytes> [--sha256 <hex>]", false)
	case "--fix-stdin":
		return stdinPayloadCommand(ModeFix, args, fmt.Sprintf("usage: glm-worker --fix-stdin <payload-bytes> [--sha256 <hex>] %s", fixOriginUsage), true)
	case "--accept":
		if len(args) != 1 {
			return Command{}, usageError("usage: glm-worker --accept")
		}
		return Command{Mode: ModeAccept}, nil
	case "--resume":
		if len(args) != 1 {
			return Command{}, usageError("usage: glm-worker --resume")
		}
		return Command{Mode: ModeResume}, nil
	case "--status":
		if len(args) != 1 {
			return Command{}, usageError("usage: glm-worker --status")
		}
		return Command{Mode: ModeStatus}, nil
	case "--watch":
		if len(args) == 2 && args[1] == "--verbose" {
			return Command{Mode: ModeWatch, WatchVerbose: true}, nil
		}
		if len(args) != 1 {
			return Command{}, usageError("usage: glm-worker --watch [--verbose]")
		}
		return Command{Mode: ModeWatch}, nil
	case "--timeline":
		if len(args) > 2 {
			return Command{}, usageError("usage: glm-worker --timeline [task-id]")
		}
		if len(args) == 2 {
			return Command{Mode: ModeTimeline, Payload: args[1]}, nil
		}
		return Command{Mode: ModeTimeline}, nil
	case "--convergence":
		if len(args) > 2 {
			return Command{}, usageError("usage: glm-worker --convergence [task-id]")
		}
		if len(args) == 2 {
			return Command{Mode: ModeConvergence, Payload: args[1]}, nil
		}
		return Command{Mode: ModeConvergence}, nil
	case "--stats":
		if len(args) != 1 {
			return Command{}, usageError("usage: glm-worker --stats")
		}
		return Command{Mode: ModeStats}, nil
	case "--reset":
		if len(args) != 1 {
			return Command{}, usageError("usage: glm-worker --reset")
		}
		return Command{Mode: ModeReset}, nil
	case "--verify-auto-resume":
		if len(args) != 4 {
			return Command{}, usageError("usage: glm-worker --verify-auto-resume <automation-key> <auto-resume-at-rfc3339> <thread-id>")
		}
		return Command{
			Mode: ModeVerifyAutoResume,
			Verify: VerifyArgs{
				Key:      args[1],
				RFC3339:  args[2],
				ThreadID: args[3],
			},
		}, nil
	case "--eval-ab":
		if len(args) != 2 {
			return Command{}, usageError("usage: glm-worker --eval-ab <run-dir>")
		}
		return Command{Mode: ModeEvalAB, Payload: args[1]}, nil
	default:
		return Command{Mode: ModeNewTask, Payload: strings.Join(args, " ")}, nil
	}
}

func stdinPayloadCommand(mode CommandMode, args []string, usage string, allowOrigin bool) (Command, error) {
	if len(args) < 2 {
		return Command{}, usageError("%s", usage)
	}

	payloadBytes, err := strconv.ParseInt(args[1], 10, 64)
	if err != nil || payloadBytes <= 0 {
		return Command{}, usageError("%s", usage)
	}

	command := Command{Mode: mode, StdinBytes: payloadBytes}
	options := args[2:]
	if len(options)%2 != 0 {
		return Command{}, usageError("%s", usage)
	}
	seenSHA256 := false
	for index := 0; index < len(options); index += 2 {
		switch options[index] {
		case "--sha256":
			if seenSHA256 {
				return Command{}, usageError("%s", usage)
			}
			digest, err := parsePayloadSHA256(options[index+1])
			if err != nil {
				return Command{}, usageError("%s", usage)
			}
			command.SHA256 = digest
			seenSHA256 = true
		case "--origin":
			if !allowOrigin || command.Origin != "" {
				return Command{}, usageError("%s", usage)
			}
			if !state.ValidParentOrigin(options[index+1]) {
				return Command{}, usageError("%s", usage)
			}
			command.Origin = options[index+1]
		default:
			return Command{}, usageError("%s", usage)
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

// StdinPayloadErrorはstdin payloadの読み取り不足・sha256不一致。process errorのkind
// stdin_payloadへ対応し、state変更・model呼出前にfail closedする。
type StdinPayloadError struct {
	Message string
}

func (e *StdinPayloadError) Error() string {
	return e.Message
}

// readStdinPayloadは宣言byte数だけstdinから読み取り、期待sha256と照合する。
// 呼び出し元のexec sessionがstdin pipeを開いたまま保つため、EOFまで読む過剰byte検知は行えない。
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

// stdinReadyControlEventはTTY stdinのpayload読み取り開始可能をcallerへ知らせる
// transport control event。raw適用成功直後にstderrへ1回だけ出し、pipe/file等の
// 非TTY stdinでは出さない。
type stdinReadyControlEvent struct {
	Type  string `json:"type"`
	Event string `json:"event"`
}

// emitStdinReadyControlEventはcontrol event行をstderrへ1回だけ書く。このevent観測が
// caller側の本文write開始条件(READY-before-write)であり、event出力はpayload読み取りの
// 前に完了する。出力時点でraw modeのためOPOSTが無効で、行末LFはwriter経路で確定観測
// できる。書込み失敗はcallerが本文を送れず永続待機するため、呼び出し元は復元を実行して
// fail closedする。
func emitStdinReadyControlEvent(w io.Writer) error {
	line, err := marshalEventLine(stdinReadyControlEvent{Type: "control", Event: "stdin_ready"})
	if err != nil {
		return fmt.Errorf("stdin ready control event encode failed: %w", err)
	}
	if _, err := w.Write(line); err != nil {
		return fmt.Errorf("stdin ready control event write failed: %w", err)
	}
	return nil
}

// テストでModelRunnerを差し替えるためのfactory。
type RunnerFactory func(cfg config.AppConfig, st *state.StateStore) workflow.ModelRunner

func defaultRunnerFactory(cfg config.AppConfig, st *state.StateStore) workflow.ModelRunner {
	return runner.NewClaudeRunner(cfg, st)
}

func Run(args []string) error {
	return run(args, config.Load, defaultRunnerFactory, os.Stdin, os.Stdout, os.Stderr)
}

func run(
	args []string,
	loadConfig func() (config.AppConfig, error),
	runnerFactory RunnerFactory,
	stdin io.Reader,
	stdout io.Writer,
	stderr io.Writer,
) error {
	cmd, err := ParseCommand(args)
	if err != nil {
		return err
	}
	if cmd.StdinBytes > 0 {
		restore, rawApplied, err := enterStdinRawMode(stdin)
		if err != nil {
			return err
		}
		if rawApplied {
			if markerErr := emitStdinReadyControlEvent(stderr); markerErr != nil {
				return errors.Join(markerErr, restore())
			}
		}
		payload, readErr := readStdinPayload(stdin, cmd.StdinBytes, cmd.SHA256)
		if err := errors.Join(readErr, restore()); err != nil {
			return err
		}
		cmd.Payload = payload
	}
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	return Execute(cmd, cfg, runnerFactory, stdout, stderr)
}

// Executeはcmdをcfg配下で実行する。runner/workflowはrf経由で注入可能で、
// --watch・--timeline・--convergence・--eval-abはstateへ書き込まないread-only参照、
// --status/--statsはロック取得前に、それ以外はプロセス間ロック後に処理する。
// stdin payload modeの読み取り・照合とTTY/PTYのtermios復元はrun()がstate初期化前に
// 完了しており、不足・不一致時はここへ到達しない。
func Execute(cmd Command, cfg config.AppConfig, rf RunnerFactory, stdout, stderr io.Writer) error {
	if cmd.StdinBytes > 0 && cmd.Payload == "" {
		return fmt.Errorf("stdin payload mode requires the payload to be read before execute")
	}
	if cmd.Mode == ModeWatch {
		return printWatch(state.AttachStateStore(cfg), stdout, defaultWatchOptions(cmd.WatchVerbose))
	}
	if cmd.Mode == ModeTimeline {
		return printTimeline(state.AttachStateStore(cfg), cmd.Payload, stdout)
	}
	if cmd.Mode == ModeConvergence {
		return printConvergence(state.AttachStateStore(cfg), cmd.Payload, stdout)
	}
	if cmd.Mode == ModeEvalAB {
		return printEvalAB(state.AttachStateStore(cfg), cmd.Payload, stdout)
	}

	st, err := state.NewStateStore(cfg)
	if err != nil {
		return err
	}

	switch cmd.Mode {
	case ModeStatus:
		return printStatus(st, stdout)
	case ModeStats:
		return printStats(st, stdout)
	case ModeVerifyAutoResume:
		return printVerifyAutoResume(cmd, cfg, stdout)
	}

	lock, err := AcquireRepoLock(st.LockPath())
	if err != nil {
		return err
	}
	defer lock.Close()

	if cmd.Mode == ModeReset {
		return resetState(st, stdout)
	}

	if cmd.Mode == ModeAccept {
		return parentAccept(st, stdout)
	}

	r := rf(cfg, st)
	wf := workflow.NewWorkflow(cfg, st, r, stdout)

	switch cmd.Mode {
	case ModeNewTask:
		return wf.ExecuteNewTask(cmd.Payload)
	case ModeDecision:
		return wf.ExecuteDecision(cmd.Payload)
	case ModeFix:
		return wf.ExecuteExplicitFix(cmd.Payload, cmd.Origin)
	case ModeResume:
		return wf.ExecuteResume()
	default:
		return fmt.Errorf("unsupported command mode")
	}
}
