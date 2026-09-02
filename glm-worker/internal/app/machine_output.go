package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/authoritybootstrapcmd"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type MachineOutputViolationError struct {
	HeldBytes int
	Cause     error
}

type singleShotOutput struct {
	target  io.Writer
	pending bytes.Buffer
}

type structuredLinesOutput struct {
	target  io.Writer
	pending bytes.Buffer
}

func Run(args []string) error {
	return runEntry(args, config.Load, instructionSurfaceRunnerFactory, os.Stdin, os.Stdout, os.Stderr)
}

func runEntry(
	args []string,
	loadConfig func() (config.AppConfig, error),
	runnerFactory RunnerFactory,
	stdin io.Reader,
	stdout io.Writer,
	stderr io.Writer,
) error {
	if handled, err := runAuthorityBootstrap(args, stdout); handled {
		return err
	}
	if handled, err := runHelp(args, stdout); handled {
		return err
	}
	return run(args, loadConfig, runnerFactory, stdin, stdout, stderr)
}

func runAuthorityBootstrap(args []string, stdout io.Writer) (bool, error) {
	if len(args) == 0 || args[0] != "--authority" {
		return false, nil
	}
	output, err := authoritybootstrapcmd.Build(args[1:])
	if err != nil {
		return true, err
	}
	return true, writeValidatedMachineJSON(stdout, output)
}

func writeValidatedMachineJSON(target io.Writer, value any) error {
	output := newSingleShotOutput(target)
	if err := writeJSON(output, value); err != nil {
		return err
	}
	return output.release()
}

func streamOutputMode(mode CommandMode) bool {
	return mode == ModeWatch
}

func dispatchMachineOutput(cmd Command, cfg config.AppConfig, rf RunnerFactory, stdout io.Writer, stderr io.Writer) error {
	if streamOutputMode(cmd.Mode) {
		return Execute(cmd, cfg, rf, stdout, stderr)
	}
	if cmd.Mode == ModeQualityGate {
		return dispatchQualityGateMachineOutput(cmd, cfg, stdout, stderr)
	}
	output := newSingleShotOutput(stdout)
	diagnostics := newStructuredLinesOutput(stderr)
	restoreWarnings := state.RedirectStatsWarnings(diagnostics)
	execErr := Execute(cmd, cfg, rf, output, diagnostics)
	restoreWarnings()
	diagnosticViolation := diagnostics.violation()
	var stdoutViolation error
	if execErr == nil {
		stdoutViolation = output.violation()
	}
	if execErr != nil || diagnosticViolation != nil || stdoutViolation != nil {
		return errors.Join(execErr, diagnosticViolation, stdoutViolation)
	}
	if err := diagnostics.release(); err != nil {
		return err
	}
	return output.release()
}

func (e *MachineOutputViolationError) Error() string {
	return fmt.Sprintf("machine stdout/stderr契約違反を検出したため出力を保留しました(%d bytes): %v", e.HeldBytes, e.Cause)
}

func newSingleShotOutput(target io.Writer) *singleShotOutput {
	return &singleShotOutput{target: target}
}

func (o *singleShotOutput) Write(p []byte) (int, error) {
	return o.pending.Write(p)
}

func (o *singleShotOutput) violation() error {
	if err := validateSingleMachineJSONObject(o.pending.Bytes()); err != nil {
		return &MachineOutputViolationError{HeldBytes: o.pending.Len(), Cause: err}
	}
	return nil
}

func (o *singleShotOutput) release() error {
	if err := o.violation(); err != nil {
		return err
	}
	data := o.pending.Bytes()
	if _, err := o.target.Write(data); err != nil {
		return fmt.Errorf("machine stdoutへの保留出力のflushに失敗しました: %w", err)
	}
	return nil
}

func newStructuredLinesOutput(target io.Writer) *structuredLinesOutput {
	return &structuredLinesOutput{target: target}
}

func (o *structuredLinesOutput) Write(p []byte) (int, error) {
	return o.pending.Write(p)
}

func (o *structuredLinesOutput) violation() error {
	if err := validateStructuredJSONLines(o.pending.Bytes()); err != nil {
		return &MachineOutputViolationError{HeldBytes: o.pending.Len(), Cause: err}
	}
	return nil
}

func (o *structuredLinesOutput) release() error {
	if err := o.violation(); err != nil {
		return err
	}
	data := o.pending.Bytes()
	if len(data) == 0 {
		return nil
	}
	if _, err := o.target.Write(data); err != nil {
		return fmt.Errorf("machine stderrへの保留出力のflushに失敗しました: %w", err)
	}
	return nil
}

func validateSingleMachineJSONObject(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return errors.New("stdoutへの出力が空です")
	}
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	var object map[string]any
	if err := decoder.Decode(&object); err != nil {
		return fmt.Errorf("stdoutが単一JSON objectとして解析できません: %w", err)
	}
	if object == nil {
		return errors.New("stdoutのtop-levelがJSON nullです")
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("stdoutに2つ目のJSON valueまたはtrailing textがあります")
	}
	return nil
}

func validateStructuredJSONLines(data []byte) error {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if len(trimmed) == 0 {
			continue
		}
		var object map[string]any
		if err := json.Unmarshal([]byte(trimmed), &object); err != nil {
			return fmt.Errorf("stderrの行がJSON objectとして解析できません: %w", err)
		}
		if object == nil {
			return errors.New("stderrの行がJSON nullです")
		}
	}
	return nil
}
