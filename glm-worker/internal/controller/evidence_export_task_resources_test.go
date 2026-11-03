package controller

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
)

func TestTaskResourceBundleWithoutGate(t *testing.T) {
	t.Parallel()
	f := newEvidenceExportFixture(t, func(f *evidenceExportFixture) { f.skipSealedEvidence = true })
	if err := os.RemoveAll(f.runtime.Path(qualitygate.RunDirectory)); err != nil {
		t.Fatal(err)
	}
	taskID := "12345678-aaaa-4bbb-8ccc-dddddddddddd"
	f.runtimeTask = taskID
	if err := f.runtime.Write("task.id", taskID); err != nil {
		t.Fatal(err)
	}
	callID := "12345678-aaaa-4bbb-8ccc-eeeeeeeeeeee"
	d := gateRunResourceFixture(strings.ReplaceAll(callID, "-", ""))
	d.Form = "task-call"
	d.Coverage.ExecutionState = "running"
	d.Coverage.EndedAt = time.Time{}
	d.Execution = &qualitygate.ResourceExecutionIdentity{TaskID: taskID, CallID: callID, SessionID: "session-worker", Role: "worker", Phase: "worker-new", Repository: f.repo,
		TaskPath: f.seal.SemanticTaskRef.TaskPath, TaskContractDigest: f.seal.SemanticTaskRef.ContractDigest, AttemptID: evidenceExportLiveAttemptID}
	name := filepath.Join("artifacts", taskID, "task-resources", callID, qualitygate.ResourceSamplesFile)
	data, err := qualitygate.EncodeResourceSamples(d)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.runtime.Write(name, string(data)); err != nil {
		t.Fatal(err)
	}
	export, _, err := f.store.ExportEvidence(EvidenceExportRequest{})
	if err != nil {
		t.Fatal(err)
	}
	entry := "live/task/artifacts/task-resources/" + callID + "/" + qualitygate.ResourceSamplesFile
	if !exportHasEntry(export.Manifest, entry) {
		t.Fatal("missing streamed resource artifact")
	}
	var index EvidenceAnalysisIndex
	if err := json.Unmarshal(exportFileByPath(t, export, "analysis-index.json"), &index); err != nil {
		t.Fatal(err)
	}
	for _, run := range index.CanonicalValidationRuns {
		if run.Form == "go-test" || run.Form == "go-test-race" {
			t.Fatal("fixture contains a gate run")
		}
	}
	if index.TaskResources.Status != "partial" {
		t.Fatalf("resource coverage: %#v", index.TaskResources)
	}
	if len(index.TaskResources.Observations) != 1 || index.TaskResources.Observations[0].Entry != entry || index.TaskResources.Observations[0].Samples != 3 || index.TaskResources.Observations[0].State != "running" {
		t.Fatalf("task resource index: %#v", index.TaskResources)
	}
	d.Execution.TaskID = "12345678-aaaa-4bbb-8ccc-ffffffffffff"
	data, err = qualitygate.EncodeResourceSamples(d)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.runtime.Write(name, string(data)); err != nil {
		t.Fatal(err)
	}
	export, _, err = f.store.ExportEvidence(EvidenceExportRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(exportFileByPath(t, export, "analysis-index.json"), &index); err != nil {
		t.Fatal(err)
	}
	if index.TaskResources.Observations[0].State != "unreadable" || index.TaskResources.Observations[0].Problem == "" {
		t.Fatal("foreign resources claimed as collected")
	}
	if err := f.runtime.Remove(name); err != nil {
		t.Fatal(err)
	}
	export, _, err = f.store.ExportEvidence(EvidenceExportRequest{})
	if err != nil {
		t.Fatal(err)
	}
	index = EvidenceAnalysisIndex{}
	if err := json.Unmarshal(exportFileByPath(t, export, "analysis-index.json"), &index); err != nil {
		t.Fatal(err)
	}
	if index.TaskResources.Status != "absent" {
		t.Fatalf("missing coverage: %#v", index.TaskResources)
	}
}

func sealedTaskResourceDocument(seal AttemptSeal, taskID string) qualitygate.ResourceSamples {
	callID := "12345678-aaaa-4bbb-8ccc-eeeeeeeeeeee"
	document := gateRunResourceFixture(strings.ReplaceAll(callID, "-", ""))
	document.Form = "task-call"
	document.Coverage.ExecutionState = "completed"
	document.Coverage.StartedAt = seal.StartedAt
	document.Coverage.EndedAt = seal.SealedAt
	for index := range document.Samples {
		document.Samples[index].At = seal.StartedAt.Add(time.Duration(index) * time.Millisecond)
		document.Samples[index].ElapsedMS = int64(index)
	}
	document.Execution = &qualitygate.ResourceExecutionIdentity{
		TaskID: taskID, CallID: callID, SessionID: "session-sealed-worker", Role: "worker", Phase: "worker-new",
		Repository: "/sealed-source", AttemptID: seal.AttemptID,
		TaskPath: seal.SemanticTaskRef.TaskPath, TaskContractDigest: seal.SemanticTaskRef.ContractDigest,
	}
	return document
}

func TestTaskResourceSealedArchiveWithoutRuntime(t *testing.T) {
	t.Parallel()
	taskID := "12345678-aaaa-4bbb-8ccc-dddddddddddd"
	fixture := newEvidenceExportFixture(t, func(f *evidenceExportFixture) {
		f.withSealedRuntime, f.withSealedResources = true, true
		f.skipLiveAttempt, f.skipRuntimeEvidence = true, true
		f.sealedAssociationRuntimeTask = taskID
	})
	export, result, err := fixture.store.ExportEvidence(EvidenceExportRequest{TaskID: taskID})
	if err != nil {
		t.Fatal(err)
	}
	if result.RuntimeStatus != evidenceExportRuntimeAbsent {
		t.Fatalf("runtime was not absent: %s", result.RuntimeStatus)
	}
	resources := decodeAnalysisIndex(t, export).TaskResources
	if len(resources.Observations) != 1 {
		t.Fatalf("sealed observations: %#v", resources)
	}
	observation := resources.Observations[0]
	if observation.Problem != "" || observation.Samples != 3 || observation.TaskID != taskID || observation.State != "completed" {
		t.Fatalf("sealed identity: %#v", observation)
	}
}

func TestTaskResourcePredecessorContractIdentity(t *testing.T) {
	t.Parallel()
	taskID := "12345678-aaaa-4bbb-8ccc-dddddddddddd"
	prior := SemanticTaskRef{TaskPath: "IMPLEMENTATION_TASKS/B.md", ContractDigest: "prior-contract"}
	document := sealedTaskResourceDocument(AttemptSeal{
		AttemptID: "prior-attempt", SemanticTaskRef: prior,
		StartedAt: time.Unix(6000, 0), SealedAt: time.Unix(6100, 0),
	}, taskID)
	data, err := qualitygate.EncodeResourceSamples(document)
	if err != nil {
		t.Fatal(err)
	}
	prefix := "predecessors/prior-attempt"
	builder := evidenceExportBuilder{
		targetTask: SemanticTaskRef{TaskPath: prior.TaskPath, ContractDigest: "current-contract"},
		attemptSection: EvidenceExportAttemptSection{Predecessors: []EvidenceExportPredecessorSection{
			{AttemptID: "prior-attempt", Task: prior, RuntimeTaskID: taskID, EntriesPrefix: prefix},
		}},
		files: []EvidenceExportFile{{Path: prefix + "/artifacts/task-resources/" + document.Execution.CallID + "/" + qualitygate.ResourceSamplesFile, Data: data}},
	}
	resources := builder.analysisTaskResources()
	if len(resources.Observations) != 1 || resources.Observations[0].Problem != "" || resources.Observations[0].Samples != 3 {
		t.Fatalf("prior contract: %#v", resources)
	}
}
