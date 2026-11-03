//go:build unix

package controller

import (
	"bytes"
	"flag"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
)

var runResourceCostProbe = flag.Bool("resource-cost-probe", false, "実ホスト採取とatomic保存のコストを測定する")

func liveMeasurementUsage(t *testing.T) (syscall.Rusage, syscall.Rusage) {
	t.Helper()
	var self, children syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &self); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Getrusage(syscall.RUSAGE_CHILDREN, &children); err != nil {
		t.Fatal(err)
	}
	return self, children
}

func liveMeasurementCPU(usage syscall.Rusage) float64 {
	return float64(usage.Utime.Sec) + float64(usage.Utime.Usec)/1e6 + float64(usage.Stime.Sec) + float64(usage.Stime.Usec)/1e6
}

func liveMeasurementExportBytes(t *testing.T, fixture *evidenceExportFixture) int64 {
	t.Helper()
	export, _, err := fixture.store.ExportEvidence(EvidenceExportRequest{})
	if err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	if err := WriteEvidenceExportZip(&archive, export); err != nil {
		t.Fatal(err)
	}
	return int64(archive.Len())
}

func TestResourceSamplingLiveMeasurement(t *testing.T) {
	if !*runResourceCostProbe {
		t.Skip("set -args -resource-cost-probe for the explicit cost probe")
	}
	fixture := newEvidenceExportFixture(t, func(f *evidenceExportFixture) { f.skipSealedEvidence = true })
	taskID := "12345678-aaaa-4bbb-8ccc-dddddddddddd"
	callID := "12345678-aaaa-4bbb-8ccc-eeeeeeeeeeee"
	if err := fixture.runtime.Write("task.id", taskID); err != nil {
		t.Fatal(err)
	}
	fixture.runtimeTask = taskID
	baselineBytes := liveMeasurementExportBytes(t, fixture)
	name := filepath.Join("artifacts", taskID, "task-resources", callID, qualitygate.ResourceSamplesFile)
	ready := make(chan struct{})
	var readyOnce sync.Once
	var publishErr error
	var writes, writeBytes int64
	var memoryBefore, memoryAfter runtime.MemStats
	runtime.ReadMemStats(&memoryBefore)
	selfBefore, childrenBefore := liveMeasurementUsage(t)
	started := time.Now()
	sampler := qualitygate.StartResourceSampler(qualitygate.ResourceSamplerConfig{
		ValidationRunID: strings.ReplaceAll(callID, "-", ""), Form: "task-call", AsyncFirst: true,
		Interval: 5 * time.Second, SampleBound: 4,
		Execution: &qualitygate.ResourceExecutionIdentity{
			TaskID: taskID, CallID: callID, SessionID: "cost-probe", Role: "worker", Phase: "cost-probe",
			Repository: fixture.repo, TaskPath: fixture.seal.SemanticTaskRef.TaskPath, TaskContractDigest: fixture.seal.SemanticTaskRef.ContractDigest,
		},
		Publish: func(document qualitygate.ResourceSamples) {
			data, err := qualitygate.EncodeResourceSamples(document)
			if err == nil {
				err = fixture.runtime.Write(name, string(data))
			}
			if err != nil {
				publishErr = err
				readyOnce.Do(func() { close(ready) })
				return
			}
			writes++
			writeBytes += int64(len(data) + 1)
			if len(document.Samples) >= 4 || document.Coverage.Status == qualitygate.ResourceCoverageUnsupported {
				readyOnce.Do(func() { close(ready) })
			}
		},
	})
	startup := time.Since(started)
	defer sampler.Stop()
	select {
	case <-ready:
	case <-time.After(40 * time.Second):
		t.Fatal("cost sampling did not reach its bound")
	}
	document := sampler.Stop()
	spent := time.Since(started)
	runtime.ReadMemStats(&memoryAfter)
	selfAfter, childrenAfter := liveMeasurementUsage(t)
	if publishErr != nil {
		t.Fatal(publishErr)
	}
	if len(document.Samples) != 4 {
		t.Fatalf("cost sample coverage: %#v", document.Coverage)
	}
	withResourcesBytes := liveMeasurementExportBytes(t, fixture)
	export, _, err := fixture.store.ExportEvidence(EvidenceExportRequest{})
	if err != nil {
		t.Fatal(err)
	}
	entry := "live/task/artifacts/task-resources/" + callID + "/" + qualitygate.ResourceSamplesFile
	bundled, err := qualitygate.DecodeResourceSamples(exportFileByPath(t, export, entry))
	if err != nil {
		t.Fatal(err)
	}
	if len(bundled.Samples) != len(document.Samples) {
		t.Fatal("cost source samples were not retained")
	}
	units := "platform-defined"
	if runtime.GOOS == "linux" {
		units = "KiB"
	}
	if runtime.GOOS == "darwin" {
		units = "bytes"
	}
	t.Logf("measurement:scope=cost-probe-only goos=%s goarch=%s startup_ms=%d sampling_ms=%d samples=%d synchronous_samples=%d synchronous_wait_ms=%d", runtime.GOOS, runtime.GOARCH, startup.Milliseconds(), spent.Milliseconds(), len(document.Samples), document.Coverage.SynchronousSamples, document.Coverage.SynchronousWaitMS)
	t.Logf("measurement:go_and_source_cpu_seconds=%.6f child_cpu_seconds=%.6f allocated_bytes=%d heap_bytes_before=%d heap_bytes_after=%d peak_rss_raw=%d peak_rss_units=%s", liveMeasurementCPU(selfAfter)-liveMeasurementCPU(selfBefore), liveMeasurementCPU(childrenAfter)-liveMeasurementCPU(childrenBefore), memoryAfter.TotalAlloc-memoryBefore.TotalAlloc, memoryBefore.HeapAlloc, memoryAfter.HeapAlloc, selfAfter.Maxrss, units)
	t.Logf("measurement:atomic_writes=%d logical_written_bytes=%d input_blocks=%d output_blocks=%d", writes, writeBytes, selfAfter.Inblock-selfBefore.Inblock, selfAfter.Oublock-selfBefore.Oublock)
	t.Logf("measurement:full_zip_increment_bytes=%d baseline_zip_bytes=%d with_resources_zip_bytes=%d", withResourcesBytes-baselineBytes, baselineBytes, withResourcesBytes)
	if withResourcesBytes <= baselineBytes {
		t.Fatal("cost archive increment was not positive")
	}
}
