package app

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type streamingStructuredLinesOutput struct {
	target  io.Writer
	pending bytes.Buffer
	failure error
}

func dispatchQualityGateMachineOutput(cmd Command, cfg config.AppConfig, stdout, stderr io.Writer) error {
	output := newSingleShotOutput(stdout)
	diagnostics := &streamingStructuredLinesOutput{target: stderr}
	restoreWarnings := state.RedirectStatsWarnings(diagnostics)
	st, storeErr := state.NewStateStore(cfg)
	var execErr error
	if storeErr == nil {
		execErr = runQualityGateWithDiagnostics(cmd.Payload, st, output, diagnostics)
	}
	restoreWarnings()
	diagnosticViolation := diagnostics.finish()
	var stdoutViolation error
	if storeErr == nil && execErr == nil {
		stdoutViolation = output.violation()
	}
	if err := errors.Join(storeErr, execErr, diagnosticViolation, stdoutViolation); err != nil {
		return err
	}
	return output.release()
}

func (o *streamingStructuredLinesOutput) Write(p []byte) (int, error) {
	if o.failure != nil {
		return 0, o.failure
	}
	written, err := o.pending.Write(p)
	if err != nil {
		return written, err
	}
	if err := o.flushCompleteLines(); err != nil {
		o.failure = err
		return written, err
	}
	return written, nil
}

func (o *streamingStructuredLinesOutput) flushCompleteLines() error {
	for {
		data := o.pending.Bytes()
		newline := bytes.IndexByte(data, '\n')
		if newline < 0 {
			return nil
		}
		line := append([]byte(nil), data[:newline+1]...)
		o.pending.Next(newline + 1)
		if err := o.writeValidated(line); err != nil {
			return err
		}
	}
}

func (o *streamingStructuredLinesOutput) finish() error {
	if o.failure != nil {
		return o.failure
	}
	if err := o.flushCompleteLines(); err != nil {
		return err
	}
	if o.pending.Len() == 0 {
		return nil
	}
	data := append([]byte(nil), o.pending.Bytes()...)
	o.pending.Reset()
	return o.writeValidated(data)
}

func (o *streamingStructuredLinesOutput) writeValidated(data []byte) error {
	if err := validateStructuredJSONLines(data); err != nil {
		return &MachineOutputViolationError{HeldBytes: len(data), Cause: err}
	}
	if _, err := o.target.Write(data); err != nil {
		return fmt.Errorf("machine stderrへのstructured出力に失敗しました: %w", err)
	}
	return nil
}
