package runner

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type fixtureTaskResourceSampler struct {
	config   qualitygate.ResourceSamplerConfig
	document qualitygate.ResourceSamples
}

func TestMain(m *testing.M) {
	startTaskResourceSampler = func(config qualitygate.ResourceSamplerConfig) taskResourceSampler {
		d := qualitygate.ResourceSamples{
			SchemaVersion: qualitygate.ResourceSamplesSchemaVersion, Form: config.Form,
			ValidationRunID: config.ValidationRunID, Execution: config.Execution,
			Coverage: qualitygate.ResourceSampleCoverage{
				Status: qualitygate.ResourceCoverageFailed, ExecutionState: "running", StartedAt: time.Now().UTC(),
				IntervalMS: 5000, SampleBound: 720, CPUState: "missing", MemoryState: "missing", SwapState: "missing",
			},
		}
		config.Publish(d)
		return &fixtureTaskResourceSampler{config: config, document: d}
	}
	os.Exit(m.Run())
}

func (s *fixtureTaskResourceSampler) StopWithState(status string) qualitygate.ResourceSamples {
	s.document.Coverage.ExecutionState = status
	s.document.Coverage.EndedAt = time.Now().UTC()
	s.config.Publish(s.document)
	return s.document
}

func TestRunnerPersistsTaskResourceIdentity(t *testing.T) {
	f := newRunnerSessionFixture(t)
	if err := f.runner.state.Write("task.id", "12345678-aaaa-4bbb-8ccc-dddddddddddd"); err != nil {
		t.Fatal(err)
	}
	r, _ := f.runFirst(t)
	taskID, err := f.runner.state.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(f.runner.state.ArtifactDir(taskID), "task-resources", r.CallID, qualitygate.ResourceSamplesFile)
	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	d, err := qualitygate.DecodeResourceSamples(data)
	if err != nil {
		t.Fatal(err)
	}
	if d.Execution.CallID != r.CallID || d.Execution.TaskID != taskID || d.Execution.SessionID != r.SessionID || d.Execution.Role != string(state.WorkerRole) || d.Execution.Phase != "worker-new" || d.Coverage.ExecutionState != "completed" {
		t.Fatalf("resource identity: %#v", d)
	}
}
