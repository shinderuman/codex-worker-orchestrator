package parentactioncmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
)

type parentExportBundleSelection struct {
	taskID string
}

type parentExportBundleRequest struct {
	Action string `json:"action"`
	TaskID string `json:"task_id,omitempty"`
}

type parentExportBundleExport struct {
	Target                       json.RawMessage `json:"target"`
	ArchivePath                  string          `json:"archive_path"`
	ManifestDigest               string          `json:"manifest_digest"`
	AttemptSection               string          `json:"attempt_section"`
	RuntimeSection               string          `json:"runtime_section"`
	Coverage                     string          `json:"coverage"`
	AuthorityChangedDuringExport bool            `json:"authority_changed_during_export"`
	ControllerGenerationBefore   uint64          `json:"controller_generation_before"`
	ControllerGenerationAfter    uint64          `json:"controller_generation_after"`
}

type parentExportBundleWorkerOutput struct {
	Action string                    `json:"action"`
	Export *parentExportBundleExport `json:"export"`
}

type parentExportBundleResult struct {
	Action string `json:"action"`
	parentExportBundleExport
}

const (
	exportBundleAction = "export-task-bundle"
	exportBundleUsage  = "usage: glm-parent-action export-bundle [--task-id <glm-worker task id>]"
)

var resolveExportBundleWorker = resolveGLMWorker

func executeExportBundleAction(cfg config.AppConfig, args []string, stdout, stderr io.Writer) error {
	selection, err := parseExportBundleSelection(args)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(parentExportBundleRequest{
		Action: exportBundleAction,
		TaskID: selection.taskID,
	})
	if err != nil {
		return fmt.Errorf("encode export bundle request: %w", err)
	}
	worker, err := resolveExportBundleWorker()
	if err != nil {
		return err
	}
	var workerOutput bytes.Buffer
	if err := runResolvedWorker(
		worker,
		cfg.RepoRoot,
		[]string{"--authority", "controller-evidence"},
		bytes.NewReader(payload),
		&workerOutput,
		stderr,
		nil,
	); err != nil {
		return err
	}
	export, err := decodeExportBundleWorkerOutput(workerOutput.Bytes())
	if err != nil {
		return err
	}
	return json.NewEncoder(stdout).Encode(parentExportBundleResult{
		Action:                   exportBundleAction,
		parentExportBundleExport: *export,
	})
}

func parseExportBundleSelection(args []string) (parentExportBundleSelection, error) {
	selection := parentExportBundleSelection{}
	for index := 1; index < len(args); index++ {
		if index+1 >= len(args) {
			return parentExportBundleSelection{}, fmt.Errorf("%s", exportBundleUsage)
		}
		flag, value := args[index], args[index+1]
		index++
		if err := applyExportBundleSelector(&selection, flag, value); err != nil {
			return parentExportBundleSelection{}, err
		}
	}
	return selection, nil
}

func applyExportBundleSelector(selection *parentExportBundleSelection, flag, value string) error {
	if value == "" {
		return fmt.Errorf("%s", exportBundleUsage)
	}
	switch flag {
	case "--task-id":
		if selection.taskID != "" {
			return fmt.Errorf("%s", exportBundleUsage)
		}
		selection.taskID = value
	default:
		return fmt.Errorf("%s", exportBundleUsage)
	}
	return nil
}

func decodeExportBundleWorkerOutput(raw []byte) (*parentExportBundleExport, error) {
	workerJSON, err := decodeSingleMachineJSON(raw, "export bundle worker output")
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(workerJSON))
	decoder.DisallowUnknownFields()
	var output parentExportBundleWorkerOutput
	if err := decoder.Decode(&output); err != nil {
		return nil, fmt.Errorf("decode export bundle worker output: %w", err)
	}
	if output.Action != exportBundleAction {
		return nil, fmt.Errorf("export bundle worker output action = %q", output.Action)
	}
	if output.Export == nil {
		return nil, errors.New("export bundle worker output has no export result")
	}
	if len(output.Export.Target) == 0 || bytes.Equal(output.Export.Target, []byte("null")) {
		return nil, errors.New("export bundle worker output has no export target")
	}
	if output.Export.ArchivePath == "" || output.Export.ManifestDigest == "" ||
		output.Export.AttemptSection == "" || output.Export.RuntimeSection == "" || output.Export.Coverage == "" {
		return nil, fmt.Errorf("export bundle worker output is incomplete: %#v", output.Export)
	}
	return output.Export, nil
}
