package runner

import (
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type taskResourceSampler interface {
	StopWithState(string) qualitygate.ResourceSamples
}

var startTaskResourceSampler = func(config qualitygate.ResourceSamplerConfig) taskResourceSampler {
	return qualitygate.StartResourceSampler(config)
}

func (r *ClaudeRunner) observeTaskResources(taskID, callID, sessionID string, role state.SessionRole, phase string, runtime *state.CallRuntime) func() {
	if !state.ValidGeneratedUUID(taskID) || !state.ValidGeneratedUUID(callID) {
		return func() {}
	}
	identity := &qualitygate.ResourceExecutionIdentity{
		TaskID: taskID, CallID: callID, SessionID: sessionID, Role: string(role), Phase: phase,
		Repository: r.config.RepoRoot,
	}
	if binding, err := r.state.LoadControllerRuntimeBinding(); err == nil {
		identity.AttemptID = binding.AttemptID
		identity.TaskPath = binding.TaskPath
		identity.TaskContractDigest = binding.TaskContractDigest
	}
	if runtime != nil {
		identity.RuntimeRevision = runtime.WorkerRevision
		identity.RuntimeModified = runtime.WorkerModified
	}
	name := filepath.Join("artifacts", taskID, "task-resources", callID, qualitygate.ResourceSamplesFile)
	var warn sync.Once
	publish := func(document qualitygate.ResourceSamples) {
		data, err := qualitygate.EncodeResourceSamples(document)
		if err == nil {
			err = os.MkdirAll(filepath.Dir(r.state.Path(name)), 0o700)
		}
		if err == nil {
			err = r.state.Write(name, string(data))
		}
		if err != nil {
			warn.Do(func() { state.WarnTaskEventSkip("resource保存", err) })
		}
	}
	sampler := startTaskResourceSampler(qualitygate.ResourceSamplerConfig{
		ValidationRunID: strings.ReplaceAll(callID, "-", ""), Form: "task-call",
		AsyncFirst: true, Execution: identity, Publish: publish,
	})
	return func() {
		status := "completed"
		if r.stop != nil && r.stop.StopRequested() {
			status = "interrupted"
		}
		sampler.StopWithState(status)
	}
}
