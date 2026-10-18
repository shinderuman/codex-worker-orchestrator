package app

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
)

func TestControllerPublicationStoresEvidenceJSONThroughMachineCLI(t *testing.T) {
	t.Parallel()
	cfg, admission, store, policy := retiredWorkflowCorpus(t, true)
	tree, err := store.PreviewCandidateTree(admission, policy)
	if err != nil {
		t.Fatal(err)
	}
	before, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	record := controller.CandidateEvidence{SchemaVersion: before.SchemaVersion, RepositoryIdentity: store.Identity().LineageID, AttemptID: admission.Attempt.AttemptID, SnapshotID: admission.Snapshot.ID, BaseOID: admission.Head.IntegrationTip, TreeOID: tree, Kind: "validation", Result: "pass"}
	command := controllerPublicationCommand{Action: "store-evidence", ExpectedGeneration: before.ControllerGeneration, EvidenceRecord: &record, ArtifactJSON: json.RawMessage(`{"validation_run_id":"actual-run","status":"pass"}`)}
	load := func() (config.AppConfig, error) { return cfg, nil }
	run := func(command controllerPublicationCommand) ([]byte, error) {
		t.Helper()
		raw, err := json.Marshal(command)
		if err != nil {
			t.Fatal(err)
		}
		var output bytes.Buffer
		err = runEntry([]string{"--authority", controllerPublicationSurface}, load, nil, bytes.NewReader(raw), &output, io.Discard)
		return output.Bytes(), err
	}
	data, err := run(command)
	if err != nil {
		t.Fatal(err)
	}
	var ref controller.EvidenceObjectRef
	if err := json.Unmarshal(data, &ref); err != nil {
		t.Fatal(err)
	}
	data, err = store.LoadEvidenceObject(ref)
	if err != nil {
		t.Fatal(err)
	}
	var stored controller.CandidateEvidence
	if err := json.Unmarshal(data, &stored); err != nil {
		t.Fatal(err)
	}
	data, err = store.LoadEvidenceObject(stored.Artifact)
	if err != nil || !bytes.Equal(data, command.ArtifactJSON) {
		t.Fatalf("CLI did not preserve actual artifact: %v", err)
	}
	for _, action := range []string{"preview", "recover", "publish"} {
		invalid := command
		invalid.Action = action
		if data, err := run(invalid); err == nil || len(data) != 0 || !strings.Contains(err.Error(), "restricted to store-evidence") {
			t.Fatalf("artifact admitted on %s: %s %v", action, data, err)
		}
	}
	command.EvidenceRecord = &stored
	if data, err := run(command); err == nil || len(data) != 0 {
		t.Fatalf("ambiguous artifact admitted: %s %v", data, err)
	}
	command.EvidenceRecord = &record
	command.ExpectedGeneration++
	if data, err := run(command); err == nil || len(data) != 0 {
		t.Fatalf("stale generation admitted: %s %v", data, err)
	}
	after, err := store.LoadHead()
	oldJSON, oldErr := json.Marshal(before)
	newJSON, newErr := json.Marshal(after)
	if err != nil || oldErr != nil || newErr != nil || !bytes.Equal(oldJSON, newJSON) {
		t.Fatalf("evidence import changed controller authority: %v", err)
	}
}
