package controller

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestPublicationRunInventoryLatestIndependentOfEnumerationOrder(t *testing.T) {
	t.Parallel()
	for _, stamps := range [][]int{{1, 1, 2}, {2, 1, 1}, {1, 2, 1}} {
		t.Run(fmt.Sprint(stamps), func(t *testing.T) {
			fixture := newFindingAcceptanceFixture(t)
			module := state.AttachStateStore(WorkflowTaskConfig(fixture.store.config, fixture.store.identity, fixture.source.Attempt.SemanticTaskRef))
			root := t.TempDir()
			for index, stamp := range stamps {
				writePublicationInventoryRecord(t, module, qualitygate.RunRecord{
					ValidationRunID: fmt.Sprintf("%032x", index+1),
					Form: "go-test", Repository: root,
					StartedAt: time.Unix(int64(stamp), 0),
					Status: qualitygate.StatusPass,
				})
				writePublicationInventoryRecord(t, module, qualitygate.RunRecord{
					ValidationRunID: fmt.Sprintf("%032x", index+4),
					Form: smokeProbeForm, Repository: root, ProbeID: "probe",
					StartedAt: time.Unix(int64(stamp), 0),
					Status: qualitygate.StatusPass,
				})
			}
			dirs, records := 0, 0
			inventory, err := readPublicationRunInventoryWith(module, func(path string) ([]os.DirEntry, error) {
				dirs++
				return os.ReadDir(path)
			}, func(store *state.StateStore, runID string) (qualitygate.RunRecord, error) {
				records++
				return qualitygate.Read(store, runID)
			})
			if err != nil {
				t.Fatal(err)
			}
			if dirs != 1 || records != 6 {
				t.Fatalf("one decision read %d directories and %d records, want 1 and 6", dirs, records)
			}
			for _, kind := range []string{"validation", "smoke"} {
				var latest qualitygate.RunRecord
				var found bool
				if kind == "validation" {
					latest, found, err = inventory.latestValidation(root, "go-test")
				} else {
					latest, found, err = inventory.latestSmokeProbe(root, "probe")
				}
				if err != nil || !found || !latest.StartedAt.Equal(time.Unix(2, 0)) {
					t.Fatalf("%s latest run depends on enumeration order: %#v found=%v err=%v", kind, latest, found, err)
				}
			}
			if dirs != 1 || records != 6 {
				t.Fatalf("selectors repeated disk history reads: directories=%d records=%d", dirs, records)
			}
		})
	}
}

func TestPublicationRunInventoryFreshReadAndNewestFailure(t *testing.T) {
	t.Parallel()
	fixture := newFindingAcceptanceFixture(t)
	module := state.AttachStateStore(WorkflowTaskConfig(fixture.store.config, fixture.store.identity, fixture.source.Attempt.SemanticTaskRef))
	root := t.TempDir()
	pass := qualitygate.RunRecord{ValidationRunID: fmt.Sprintf("%032x", 1), Form: "go-test", Repository: root, StartedAt: time.Unix(1, 0), Status: qualitygate.StatusPass}
	writePublicationInventoryRecord(t, module, pass)
	before, err := readPublicationRunInventory(module)
	if err != nil {
		t.Fatal(err)
	}
	failure := qualitygate.RunRecord{ValidationRunID: fmt.Sprintf("%032x", 2), Form: "go-test", Repository: root, StartedAt: time.Unix(2, 0), Status: qualitygate.StatusFail}
	writePublicationInventoryRecord(t, module, failure)
	oldRun, _, err := before.latestValidation(root, "go-test")
	if err != nil || oldRun.ValidationRunID != pass.ValidationRunID {
		t.Fatalf("existing decision unexpectedly mutated: run=%#v err=%v", oldRun, err)
	}
	after, err := readPublicationRunInventory(module)
	if err != nil {
		t.Fatal(err)
	}
	newRun, found, err := after.latestValidation(root, "go-test")
	if err != nil || !found || newRun.ValidationRunID != failure.ValidationRunID || newRun.Status != qualitygate.StatusFail {
		t.Fatalf("fresh decision reused old PASS: run=%#v found=%v err=%v", newRun, found, err)
	}
}

func TestPublicationRunInventoryRejectsOnlyLatestTie(t *testing.T) {
	t.Parallel()
	fixture := newFindingAcceptanceFixture(t)
	module := state.AttachStateStore(WorkflowTaskConfig(fixture.store.config, fixture.store.identity, fixture.source.Attempt.SemanticTaskRef))
	root := t.TempDir()
	for index, stamp := range []int{2, 2, 1} {
		for _, form := range []string{"go-test", smokeProbeForm} {
			id := index + 1
			if form == smokeProbeForm {
				id += 3
			}
			writePublicationInventoryRecord(t, module, qualitygate.RunRecord{
				ValidationRunID: fmt.Sprintf("%032x", id), Form: form, Repository: root, ProbeID: "probe",
				StartedAt: time.Unix(int64(stamp), 0),
			})
		}
	}
	inventory, err := readPublicationRunInventory(module)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := inventory.latestValidation(root, "go-test"); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("latest validation time tie was accepted: %v", err)
	}
	if _, _, err := inventory.latestSmokeProbe(root, "probe"); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("latest smoke time tie was accepted: %v", err)
	}
	if _, found, err := inventory.latestSmokeProbe(root, "unrelated"); err != nil || found {
		t.Fatalf("unrelated probe should be absent: found=%v err=%v", found, err)
	}
}

func TestPublicationRunInventoryMalformedUnrelatedFailsClosed(t *testing.T) {
	t.Parallel()
	fixture := newFindingAcceptanceFixture(t)
	module := state.AttachStateStore(WorkflowTaskConfig(fixture.store.config, fixture.store.identity, fixture.source.Attempt.SemanticTaskRef))
	root := t.TempDir()
	writePublicationInventoryRecord(t, module, qualitygate.RunRecord{
		ValidationRunID: fmt.Sprintf("%032x", 1), Form: "go-test", Repository: root, StartedAt: time.Unix(1, 0),
	})
	badID := fmt.Sprintf("%032x", 2)
	badPath := module.Path(qualitygate.RunRelativePath(badID))
	if err := os.MkdirAll(filepath.Dir(badPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(badPath, []byte("{bad-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readPublicationRunInventory(module); err == nil || !strings.Contains(err.Error(), badID) {
		t.Fatalf("malformed unrelated record escaped fail-closed read: %v", err)
	}
}

func writePublicationInventoryRecord(t *testing.T, module *state.StateStore, record qualitygate.RunRecord) {
	t.Helper()
	data, err := qualitygate.Encode(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := module.Write(qualitygate.RunRelativePath(record.ValidationRunID), string(data)); err != nil {
		t.Fatal(err)
	}
}
