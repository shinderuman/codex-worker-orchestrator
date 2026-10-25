package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

var runCompletionInstaller = runFixedCompletionInstaller

func runFixedCompletionInstaller(root, sourceOID string, output io.Writer) (controller.InstalledRuntimeIdentity, error) {
	install := exec.Command(filepath.Join(root, "install.sh"))
	install.Dir = root
	install.Stdout, install.Stderr = output, output
	if err := install.Run(); err != nil {
		return controller.InstalledRuntimeIdentity{}, err
	}
	bin := os.Getenv("GLM_WORKER_BIN_DIR")
	if bin == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return controller.InstalledRuntimeIdentity{}, err
		}
		bin = filepath.Join(home, ".local", "bin")
	}
	return installedCompletionRuntime(filepath.Join(bin, "glm-worker"), root, sourceOID)
}

func installedCompletionRuntime(path, root, sourceOID string) (controller.InstalledRuntimeIdentity, error) {
	command := exec.Command(path, "--status")
	command.Dir = root
	output, err := command.Output()
	if err != nil {
		return controller.InstalledRuntimeIdentity{}, fmt.Errorf("installed runtime status: %w", err)
	}
	var status struct {
		Build statusRuntimeBuild `json:"runtime_build"`
	}
	if err := json.Unmarshal(output, &status); err != nil {
		return controller.InstalledRuntimeIdentity{}, err
	}
	build := status.Build
	if build.VCSRevision == nil || *build.VCSRevision != sourceOID || build.VCSModified == nil || *build.VCSModified || build.RepositoryHead == nil || *build.RepositoryHead != sourceOID || build.Relationship != runtimeBuildSame {
		return controller.InstalledRuntimeIdentity{}, fmt.Errorf("installed runtime build does not match the clean final candidate")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return controller.InstalledRuntimeIdentity{}, err
	}
	sum := sha256.Sum256(data)
	absolute, err := filepath.Abs(path)
	if err != nil {
		return controller.InstalledRuntimeIdentity{}, err
	}
	return controller.InstalledRuntimeIdentity{SourceOID: sourceOID, BinaryPath: absolute, BinarySHA256: hex.EncodeToString(sum[:])}, nil
}

func executeParentCompletionInstall(cfg config.AppConfig, store *controller.Store, input controller.PublicationInput, command controllerPublicationCommand) (result any, resultErr error) {
	candidate, root, release, err := store.CandidateInstallWorkspace(input)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, release()) }()
	if command.SourceOID != candidate.CommitOID {
		return nil, fmt.Errorf("installation command source is stale")
	}
	capture := &installSmokeCapture{}
	installed, err := runCompletionInstaller(root, candidate.CommitOID, capture)
	if err != nil {
		ref, recordErr := store.PutEvidenceObject("completion-install-failure", "text/plain", candidate.CommitOID, true, capture.evidence())
		return nil, errors.Join(fmt.Errorf("completion install failed: %w; evidence=%s", err, ref.Digest), recordErr)
	}
	taskCfg := controller.WorkflowTaskConfig(cfg, store.Identity(), candidate.TaskRef)
	runtime := state.AttachStateStore(taskCfg)
	smokeCfg := cfg
	smokeCfg.RepoRoot = root
	var output bytes.Buffer
	if err := runInstallSmoke("parent", smokeCfg, runtime, &output); err != nil {
		return nil, err
	}
	var smoke installSmokeOutput
	if err := json.Unmarshal(output.Bytes(), &smoke); err != nil {
		return nil, err
	}
	return store.RecordParentCompletionInstall(input, root, filepath.Base(filepath.Dir(smoke.Evidence)), installed, capture.evidence())
}
