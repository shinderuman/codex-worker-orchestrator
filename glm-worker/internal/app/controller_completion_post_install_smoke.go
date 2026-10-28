package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type postInstallSmokeRunOutput struct {
	ProbeID                string `json:"probe_id"`
	ValidationRunID        string `json:"validation_run_id"`
	Status                 string `json:"status"`
	ExitCode               int    `json:"exit_code"`
	DurationMS             int64  `json:"duration_ms"`
	InstallEvidenceBinding string `json:"install_evidence_digest"`
	Evidence               string `json:"evidence"`
}

type postInstallSmokeOutput struct {
	Status    string                      `json:"status"`
	Result    string                      `json:"result"`
	ProbeRuns []postInstallSmokeRunOutput `json:"probe_runs"`
}

func executeParentCompletionPostInstallSmoke(cfg config.AppConfig, store *controller.Store, input controller.PublicationInput, command controllerPublicationCommand) (result any, resultErr error) {
	candidate, root, release, err := preparePostInstallSmoke(cfg, store, input, command.SourceOID)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, release()) }()
	runtime := state.AttachStateStore(controller.WorkflowTaskConfig(cfg, store.Identity(), candidate.TaskRef))
	probes, err := store.OutstandingSmokeProbes(candidate)
	if err != nil {
		return nil, err
	}
	if len(probes) == 0 {
		return nil, fmt.Errorf("candidate has no outstanding post-install smoke probes")
	}
	runs := make([]postInstallSmokeRunOutput, 0, len(probes))
	failed := ""
	for _, probe := range probes {
		record, runErr := runPostInstallSmokeProbe(store, candidate, probe, root, runtime)
		if runErr != nil {
			return nil, runErr
		}
		runs = append(runs, postInstallSmokeRunOutput{ProbeID: probe.ProbeID, ValidationRunID: record.ValidationRunID, Status: record.Status, ExitCode: record.ExitCode, DurationMS: record.DurationMS, InstallEvidenceBinding: record.InstallEvidenceDigest, Evidence: record.Log})
		if record.Status != qualitygate.StatusPass && failed == "" {
			failed = fmt.Sprintf("post-install smoke probe %s failed (exit %d); evidence=%s", probe.ProbeID, record.ExitCode, record.Log)
		}
	}
	if failed != "" {
		return nil, errors.New(failed)
	}
	return postInstallSmokeOutput{Status: "executed", Result: "pass", ProbeRuns: runs}, nil
}

func preparePostInstallSmoke(cfg config.AppConfig, store *controller.Store, input controller.PublicationInput, sourceOID string) (controller.AcceptedCandidate, string, func() error, error) {
	candidate, root, release, err := store.CandidatePostInstallSmokeWorkspace(input)
	if err != nil {
		return candidate, "", nil, err
	}
	if sourceOID != candidate.CommitOID {
		return candidate, "", nil, fmt.Errorf("post-install smoke command source is stale")
	}
	runtime := state.AttachStateStore(controller.WorkflowTaskConfig(cfg, store.Identity(), candidate.TaskRef))
	if err := reconcilePostInstallSmokeRuns(runtime, root); err != nil {
		return candidate, "", nil, err
	}
	interrupted, err := store.InterruptedPostInstallSmokeWorkspace(candidate)
	if err != nil {
		return candidate, "", nil, err
	}
	if err := store.PreparePostInstallSmokeCheckout(candidate, interrupted); err != nil {
		return candidate, "", nil, err
	}
	return candidate, root, release, nil
}

func runPostInstallSmokeProbe(store *controller.Store, candidate controller.AcceptedCandidate, probe controller.SmokeProbeDeclaration, root string, runtime *state.StateStore) (qualitygate.RunRecord, error) {
	record, err := store.NewSmokeProbeRunRecord(candidate, probe, root, runtime.ReadOr("task.id", ""))
	if err != nil {
		return record, err
	}
	deployment, err := store.PostInstallSmokeDeployment(candidate)
	if err != nil {
		return record, err
	}
	if err := writePostInstallSmokeRecord(runtime, record); err != nil {
		return record, err
	}
	capture := &installSmokeCapture{}
	command := exec.Command(probe.Argv[0], probe.Argv[1:]...)
	command.Dir = root
	command.Env = append(os.Environ(),
		"GLM_POST_INSTALL_BINARY="+deployment.BinaryPath,
		"GLM_POST_INSTALL_SOURCE_OID="+deployment.SourceOID,
		"GLM_POST_INSTALL_EVIDENCE_DIGEST="+deployment.EvidenceDigest,
	)
	command.Stdout, command.Stderr = capture, capture
	runErr := runPostInstallSmokeCommand(command, runtime, &record)
	completed := time.Now().UTC()
	record.CompletedAt = &completed
	record.DurationMS = completed.Sub(record.StartedAt).Milliseconds()
	record.Status = qualitygate.StatusPass
	record.ExitCode = 0
	record.ExitSource = state.ValidationExitSourceTarget
	log := capture.evidence()
	if runErr != nil {
		record.Status = qualitygate.StatusFail
		record.ExitCode = 1
		record.ExitSource = state.ValidationExitSourceWrapper
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			record.ExitCode = exitErr.ExitCode()
			record.ExitSource = state.ValidationExitSourceTarget
			if status, ok := exitErr.Sys().(syscall.WaitStatus); ok && status.Signaled() {
				record.Status = qualitygate.StatusInterrupted
			}
		}
	} else if sourceErr := store.VerifyPostInstallSmokeSource(candidate, root); sourceErr != nil {
		record.Status = qualitygate.StatusFail
		record.ExitCode = 1
		record.ExitSource = state.ValidationExitSourceWrapper
		log = append(log, []byte("\npost-install smoke source verification failed: "+sourceErr.Error()+"\n")...)
	}
	logPath := filepath.Join(runtime.Path(qualitygate.RunDirectory), record.ValidationRunID, "probe.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		return record, err
	}
	if err := os.WriteFile(logPath, log, 0o600); err != nil {
		return record, err
	}
	record.Log = logPath
	return record, writePostInstallSmokeRecord(runtime, record)
}

func writePostInstallSmokeRecord(runtime *state.StateStore, record qualitygate.RunRecord) error {
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	return runtime.Write(qualitygate.RunRelativePath(record.ValidationRunID), string(data))
}

func runPostInstallSmokeCommand(command *exec.Cmd, runtime *state.StateStore, record *qualitygate.RunRecord) error {
	if err := command.Start(); err != nil {
		return err
	}
	record.RunnerPID = command.Process.Pid
	if err := writePostInstallSmokeRecord(runtime, *record); err != nil {
		return errors.Join(err, command.Process.Kill(), command.Wait())
	}
	return command.Wait()
}

func reconcilePostInstallSmokeRuns(runtime *state.StateStore, root string) error {
	entries, err := os.ReadDir(runtime.Path(qualitygate.RunDirectory))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() || !qualitygate.ValidRunID(entry.Name()) {
			continue
		}
		record, err := qualitygate.Read(runtime, entry.Name())
		if err != nil {
			return err
		}
		if record.Form != controller.PublicationOperationPostInstallSmoke || record.Repository != root || record.Status != qualitygate.StatusRunning {
			continue
		}
		record, err = reconcileQualityGateRun(runtime, record.ValidationRunID)
		if err != nil {
			return err
		}
		if record.Status == qualitygate.StatusRunning {
			return fmt.Errorf("post-install smoke probe %s is still running; retain the current candidate", record.ProbeID)
		}
	}
	return nil
}
