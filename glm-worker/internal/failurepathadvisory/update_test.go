package failurepathadvisory

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repolock"
)

func labeledSeedRegistry() Registry {
	return Registry{}.WithRecord(observedRecord("task-a",
		Finding{Target: "a:1", Class: ClassExternalModelInvocation, Issue: "i"},
	))
}

func labelsForTaskA() RegistryUpdate {
	return func(registry Registry) (Registry, error) {
		return ApplyLabels(registry, LabelInput{
			Schema: LabelsSchema,
			TaskID: "task-a",
			FindingDispositions: []FindingDispositionInput{
				{Index: 0, Disposition: DispositionTruePositive},
			},
			AvoidedReviewFixWaves: 1,
			Usage:                 &UsageComparison{Measured: true},
		})
	}
}

func TestAppendRecordPreservesConcurrentLabelWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), RegistryFile)
	if err := SaveRegistry(path, labeledSeedRegistry()); err != nil {
		t.Fatal(err)
	}
	stale, err := LoadRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := UpdateRegistry(path, labelsForTaskA()); err != nil {
		t.Fatal(err)
	}

	if _, err := AppendRecord(path, observedRecord("task-b")); err != nil {
		t.Fatal(err)
	}
	final, err := LoadRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(final.Records) != 2 {
		t.Fatalf("records = %+v", final.Records)
	}
	if final.Records[0].Findings[0].Label == nil || final.Records[0].Assessment == nil {
		t.Fatalf("並行labels適用がappendで失われました: %+v", final.Records[0])
	}
	if final.Records[1].TaskID != "task-b" {
		t.Fatalf("append record = %+v", final.Records[1])
	}
	if len(stale.Records) != 1 || stale.Records[0].Findings[0].Label != nil {
		t.Fatalf("stale読み取りが書き戻しに使われました: %+v", stale.Records)
	}
}

func TestUpdateRegistryConcurrentWritersKeepBothUpdates(t *testing.T) {
	path := filepath.Join(t.TempDir(), RegistryFile)
	if err := SaveRegistry(path, labeledSeedRegistry()); err != nil {
		t.Fatal(err)
	}

	var wait sync.WaitGroup
	wait.Add(2)
	go func() {
		defer wait.Done()
		if _, err := AppendRecord(path, observedRecord("task-b")); err != nil {
			t.Errorf("並行append: %v", err)
		}
	}()
	go func() {
		defer wait.Done()
		if _, err := UpdateRegistry(path, labelsForTaskA()); err != nil {
			t.Errorf("並行labels: %v", err)
		}
	}()
	wait.Wait()

	final, err := LoadRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(final.Records) != 2 {
		t.Fatalf("並行書込でrecordが失われました: %+v", final.Records)
	}
	if final.Records[0].Findings[0].Label == nil {
		t.Fatalf("並行書込でlabelが失われました: %+v", final.Records[0])
	}
}

func TestUpdateRegistryRejectsCorruptRegistryWithoutOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), RegistryFile)
	corrupt := []byte(`{"schema":`)
	if err := os.WriteFile(path, corrupt, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := UpdateRegistry(path, func(registry Registry) (Registry, error) {
		return registry.WithRecord(observedRecord("task-x")), nil
	}); err == nil {
		t.Fatal("corrupt registryが空registry扱いになりました")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(corrupt) {
		t.Fatalf("corrupt registryが上書きされました: %q", after)
	}
}

func fullCohortRegistry() Registry {
	registry := Registry{}
	for index := 0; index < CohortCap; index++ {
		registry = registry.WithRecord(observedRecord(fmtTaskID(index)))
	}
	return registry
}

func fmtTaskID(index int) string {
	return "cohort-" + string(rune('a'+index%26)) + string(rune('0'+index/26))
}

func TestAppendRecordKeepsCohortWithinCapUnderLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), RegistryFile)
	if err := SaveRegistry(path, fullCohortRegistry()); err != nil {
		t.Fatal(err)
	}

	if _, err := AppendRecord(path, observedRecord("task-over")); !errors.Is(err, ErrCohortFull) {
		t.Fatalf("上限超過recordのerror = %v", err)
	}
	registry, err := LoadRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	if registry.CohortSize() != CohortCap || registry.HasTaskRecord("task-over") {
		t.Fatalf("cohort = %d records = %+v", registry.CohortSize(), registry.Records)
	}

	if _, err := AppendRecord(path, Record{TaskID: "task-capped-marker", Outcome: OutcomeCapped}); err != nil {
		t.Fatalf("非cohort recordのappend error = %v", err)
	}
	registry, err = LoadRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	if registry.CohortSize() != CohortCap || !registry.HasTaskRecord("task-capped-marker") {
		t.Fatalf("非cohort recordが拒否されました: cohort=%d", registry.CohortSize())
	}
}

func TestAppendRecordConcurrentRaceKeepsCohortBounded(t *testing.T) {
	path := filepath.Join(t.TempDir(), RegistryFile)
	registry := Registry{}
	for index := 0; index < CohortCap-1; index++ {
		registry = registry.WithRecord(observedRecord(fmtTaskID(index)))
	}
	if err := SaveRegistry(path, registry); err != nil {
		t.Fatal(err)
	}

	var wait sync.WaitGroup
	results := make([]error, 2)
	wait.Add(2)
	for position, taskID := range []string{"task-race-a", "task-race-b"} {
		record := observedRecord(taskID)
		go func(position int, record Record) {
			defer wait.Done()
			_, results[position] = AppendRecord(path, record)
		}(position, record)
	}
	wait.Wait()

	final, err := LoadRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	if final.CohortSize() != CohortCap {
		t.Fatalf("並行append後cohort = %d want %d", final.CohortSize(), CohortCap)
	}
	refused := 0
	for _, result := range results {
		if errors.Is(result, ErrCohortFull) {
			refused++
		}
	}
	if refused != 1 {
		t.Fatalf("拒否数 = %d results = %v", refused, results)
	}
}

func TestUpdateRegistryLockContentionIsBoundedWithoutWrite(t *testing.T) {
	previousWait := registryLockWait
	previousInterval := registryLockRetryInterval
	registryLockWait = 20 * time.Millisecond
	registryLockRetryInterval = 5 * time.Millisecond
	t.Cleanup(func() {
		registryLockWait = previousWait
		registryLockRetryInterval = previousInterval
	})

	path := filepath.Join(t.TempDir(), RegistryFile)
	if err := SaveRegistry(path, labeledSeedRegistry()); err != nil {
		t.Fatal(err)
	}
	lock, err := repolock.Acquire(path + ".lock")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lock.Close() }()

	if _, err := UpdateRegistry(path, labelsForTaskA()); err == nil {
		t.Fatal("lock競合でerrorが返りませんでした")
	}
	registry, err := LoadRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.Records) != 1 || registry.Records[0].Findings[0].Label != nil {
		t.Fatalf("lock競合中にregistryが変更されました: %+v", registry.Records)
	}
}
