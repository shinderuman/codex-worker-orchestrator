//go:build unix

package controller

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func liveMeasurementCPUSpent() (float64, float64) {
	var self, children syscall.Rusage
	if syscall.Getrusage(syscall.RUSAGE_SELF, &self) != nil || syscall.Getrusage(syscall.RUSAGE_CHILDREN, &children) != nil {
		return 0, 0
	}
	spent := func(usage syscall.Rusage) float64 {
		return float64(usage.Utime.Sec) + float64(usage.Utime.Usec)/1e6 +
			float64(usage.Stime.Sec) + float64(usage.Stime.Usec)/1e6
	}
	return spent(self), spent(children)
}

func liveMeasurementExportBytes(t *testing.T, fixture *evidenceExportFixture) int64 {
	t.Helper()
	export, _, err := fixture.store.ExportEvidence(EvidenceExportRequest{})
	if err != nil {
		t.Fatal(err)
	}
	total := int64(0)
	for _, file := range export.Files {
		total += int64(len(file.Data))
	}
	return total
}

func TestResourceSamplingLiveMeasurement(t *testing.T) {
	if os.Getenv("GLM_RESOURCE_MEASUREMENT") != "1" {
		t.Skip("set GLM_RESOURCE_MEASUREMENT=1 to run the live host resource sampling measurement")
	}
	fixture := newEvidenceExportFixture(t)
	repoRoot, err := canonicalPath(fixture.repo)
	if err != nil {
		t.Fatal(err)
	}
	run := regressionFixtureRun(repoRoot, "3a3a3a3a3a3a3a3a3a3a3a3a3a3a3a3a", time.Unix(6100, 0).UTC())
	fixture.writeModuleValidationRun(t, run)
	baselineBytes := liveMeasurementExportBytes(t, fixture)

	selfBefore, childrenBefore := liveMeasurementCPUSpent()
	samplingStart := time.Now()
	sampler := qualitygate.StartResourceSampler(qualitygate.ResourceSamplerConfig{
		ValidationRunID: run.ValidationRunID,
		Form:            run.Form,
		Interval:        3 * time.Second,
		SampleBound:     4,
	})
	firstSampleSpent := time.Since(samplingStart)
	time.Sleep(9500 * time.Millisecond)
	document := sampler.Stop()
	samplingSpent := time.Since(samplingStart)
	selfAfter, childrenAfter := liveMeasurementCPUSpent()

	data, err := qualitygate.EncodeResourceSamples(document)
	if err != nil {
		t.Fatalf("live document does not encode: %v", err)
	}
	if len(document.Samples) == 0 {
		t.Fatalf("live sampling captured no ticks: %#v", document)
	}
	writeEvidenceGateResourcesContent(t, fixture, run.ValidationRunID, string(data))
	withResourcesBytes := liveMeasurementExportBytes(t, fixture)

	t.Logf("measurement:first_sample_ms=%d", firstSampleSpent.Milliseconds())
	t.Logf("measurement:total_sampling_ms=%d samples=%d per_tick_ms=%.1f",
		samplingSpent.Milliseconds(), len(document.Samples),
		float64(samplingSpent.Milliseconds()-firstSampleSpent.Milliseconds())/float64(len(document.Samples)-1))
	t.Logf("measurement:process_cpu_seconds=%.3f child_cpu_seconds=%.3f",
		selfAfter-selfBefore, childrenAfter-childrenBefore)
	t.Logf("measurement:document_bytes=%d coverage_status=%s cpu=%s memory=%s swap=%s",
		len(data), document.Coverage.Status, document.Coverage.CPUState,
		document.Coverage.MemoryState, document.Coverage.SwapState)
	t.Logf("measurement:bundle_increment_bytes=%d baseline_bytes=%d with_resources_bytes=%d",
		withResourcesBytes-baselineBytes, baselineBytes, withResourcesBytes)
	for _, problem := range document.Coverage.Problems {
		t.Logf("measurement:problem=%q", problem)
	}

	canonical, err := canonicalPath(fixture.repo)
	if err != nil {
		t.Fatal(err)
	}
	module := state.AttachStateStore(config.AppConfig{StateBase: fixture.cfg.StateBase, RepoHash: config.RepoHashFor(canonical)})
	path := module.Path(filepath.Join(qualitygate.RunDirectory, run.ValidationRunID, qualitygate.ResourceSamplesFile))
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("sampled document was not collected into the export source: %v", err)
	}
	entry := "live/validation/quality-gate/" + run.ValidationRunID + "/" + qualitygate.ResourceSamplesFile
	export, _, err := fixture.store.ExportEvidence(EvidenceExportRequest{})
	if err != nil {
		t.Fatal(err)
	}
	bundled, err := qualitygate.DecodeResourceSamples(exportFileByPath(t, export, entry))
	if err != nil {
		t.Fatalf("bundled live document does not decode: %v", err)
	}
	if bundled.ValidationRunID != run.ValidationRunID || len(bundled.Samples) != len(document.Samples) {
		t.Fatalf("bundled live document = %#v", bundled)
	}
	if withResourcesBytes-baselineBytes <= 0 {
		t.Fatalf("bundle increment was not positive: %d", withResourcesBytes-baselineBytes)
	}
}
