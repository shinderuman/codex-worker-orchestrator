package controller

import (
	"testing"
)

func TestExportEvidenceRequiresRuntimeTaskIdentityForDefaultTargets(t *testing.T) {
	t.Parallel()
	t.Run("live attempt without a runtime binding", func(t *testing.T) {
		fixture := newEvidenceExportFixture(t, func(f *evidenceExportFixture) {
			f.skipRuntimeEvidence = true
		})
		if _, _, err := fixture.store.ExportEvidence(EvidenceExportRequest{}); err == nil {
			t.Fatal("export accepted a live attempt without a resolvable runtime task identity")
		}
	})
	t.Run("recent sealed attempt without a canonical association", func(t *testing.T) {
		fixture := newEvidenceExportFixture(t, func(f *evidenceExportFixture) {
			f.skipLiveAttempt = true
			f.skipRuntimeEvidence = true
		})
		if _, _, err := fixture.store.ExportEvidence(EvidenceExportRequest{}); err == nil {
			t.Fatal("export accepted a sealed attempt without a canonical runtime task association")
		}
	})
}


func TestExportEvidenceRejectsUnknownTargetsAndSelectors(t *testing.T) {
	t.Parallel()
	fixture := newEvidenceExportFixture(t)
	if _, _, err := fixture.store.ExportEvidence(EvidenceExportRequest{TaskID: "runtime-task-unknown"}); err == nil {
		t.Fatal("export accepted an unknown runtime task id")
	}
	if _, _, err := fixture.store.ExportEvidence(EvidenceExportRequest{TaskID: "../escape"}); err == nil {
		t.Fatal("export accepted an unsafe runtime task id")
	}
	empty := newEvidenceExportFixture(t, func(f *evidenceExportFixture) {
		f.skipSealedEvidence = true
		f.skipLiveAttempt = true
		f.skipRuntimeEvidence = true
	})
	if _, _, err := empty.store.ExportEvidence(EvidenceExportRequest{}); err == nil {
		t.Fatal("export accepted a repository without executed tasks")
	}
}

func TestExportEvidenceTaskIDResolvesSurvivingRuntimeBinding(t *testing.T) {
	t.Parallel()
	fixture := newEvidenceExportFixture(t, func(f *evidenceExportFixture) {
		f.skipLiveHead = true
	})
	export, result, err := fixture.store.ExportEvidence(EvidenceExportRequest{TaskID: fixture.runtimeTask})
	if err != nil {
		t.Fatal(err)
	}
	if result.Target.SelectionBasis != evidenceExportBasisRuntimeBinding || result.Target.AttemptID != evidenceExportLiveAttemptID {
		t.Fatalf("runtime binding target = %#v", result.Target)
	}
	if result.RuntimeStatus != evidenceExportRuntimeCollected || export.Manifest.Runtime.Mode != evidenceExportRuntimeModeBound {
		t.Fatalf("non-live binding target runtime section = %#v", export.Manifest.Runtime)
	}
	if result.Coverage != evidenceExportCoveragePartial {
		t.Fatalf("non-live binding target coverage = %q", result.Coverage)
	}
	if export.Manifest.Runtime.Basis != evidenceExportBasisRuntimeBinding {
		t.Fatalf("bound runtime section basis = %q", export.Manifest.Runtime.Basis)
	}
	if export.Manifest.Runtime.Window == nil || export.Manifest.Runtime.Window.EndBasis != evidenceExportBoundNoEndBoundary ||
		!export.Manifest.Runtime.Window.End.IsZero() {
		t.Fatalf("unsealed bound runtime window = %#v", export.Manifest.Runtime.Window)
	}
	boundGate := exportEntryByPath(t, export.Manifest, "bound/validation/quality-gate/"+evidenceExportGateRunID+"/run.json")
	if boundGate.Basis != evidenceExportBasisRuntimeValidation || boundGate.Missing {
		t.Fatalf("bound gate run entry = %#v", boundGate)
	}
	if !exportHasEntry(export.Manifest, "bound/validation/install-smoke/"+evidenceExportSmokeRunID+"/smoke.log") {
		t.Fatal("bound export lost the install smoke evidence")
	}
	boundPlan := exportEntryByPath(t, export.Manifest, "bound/"+evidenceExportSnapshotEntryDir+"/"+evidenceExportInstructionPlanPath)
	if boundPlan.Unreadable != evidenceExportInstructionNotSealedReason {
		t.Fatalf("bound plan snapshot entry = %#v", boundPlan)
	}
	boundTelemetry := exportEntryByPath(t, export.Manifest, "bound/task/telemetry.jsonl")
	if boundTelemetry.InProgress || boundTelemetry.Basis != evidenceExportBasisLiveRuntime {
		t.Fatalf("bound telemetry entry = %#v", boundTelemetry)
	}
	liveTelemetry := false
	for _, entry := range export.Manifest.Entries {
		if entry.Path == "live/task/telemetry.jsonl" {
			liveTelemetry = true
		}
	}
	if liveTelemetry {
		t.Fatal("bound export wrote in-progress live entries")
	}
}
